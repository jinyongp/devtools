import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { EventEmitter } from 'node:events';
import { lookup } from 'node:dns/promises';
import fs from 'node:fs/promises';
import { readFileSync, realpathSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import { chromium, firefox, webkit } from '@playwright/test';

const config = JSON.parse(await fs.readFile(process.argv[2], 'utf8'));
const output = config.artifacts;
const origin = new URL(config.origin);
const timeout = 90_000;
const events = new EventEmitter();
const started = Date.now();
const trace = [];
const traceEvent = (type, data = {}) => trace.push({ elapsed_ms: Date.now() - started, type, ...data });
const report = { case: config.case, browser: config.browser, origin: origin.origin, policy: config.policy, websocketUrls: [], console: [], rpc: [], driverNode: process.version };
const redact = text => String(text).replace(/authorization\s*[:=]\s*(?:Basic|Bearer)\s+[^\s,"']+/gi, 'authorization=[redacted]').replace(/([?&](?:token|auth|key|sid)=)[^&\s"']+/gi, '$1[redacted]');
let parseRpc;
let stringifyRpc;
let NuxtSocket;
let nuxtSocketUrl;
let nuxtSocketAddress;
if (config.case === 'nuxt') {
  const require = createRequire(realpathSync(path.join(config.fixturePackages, '@nuxt/devtools/package.json')));
  ({ parse: parseRpc, stringify: stringifyRpc } = await import(pathToFileURL(require.resolve('structured-clone-es'))));
  const socketRequire = createRequire(realpathSync(path.join(config.fixturePackages, 'ws/package.json')));
  NuxtSocket = socketRequire('ws');
}
async function probeNuxtOrigin(originHeader, expectsRpc) {
  return new Promise(resolve => {
    const id = `probe-${Math.random()}`;
    const options = {
      family: 4,
      lookup: (hostname, _options, done) => {
        if (hostname !== origin.hostname) return done(new Error('RPC probe used an unexpected hostname'));
        done(null, nuxtSocketAddress.address, nuxtSocketAddress.family);
      },
    };
    if (originHeader !== undefined) options.origin = originHeader;
    if (config.ca) options.ca = readFileSync(config.ca);
    if (process.env.PROXY_COMPAT_AUTH) {
      const auth = JSON.parse(process.env.PROXY_COMPAT_AUTH);
      options.headers = { Authorization: `Basic ${Buffer.from(`${auth.username}:${auth.password}`).toString('base64')}` };
    }
    const socket = new NuxtSocket(nuxtSocketUrl, 'vite-hmr', options);
    let finished = false;
    const finish = result => { if (finished) return; finished = true; clearTimeout(timer); socket.terminate(); resolve(result); };
    let timer = setTimeout(() => finish('handshake-timeout'), 20_000);
    socket.on('error', error => finish(`rejected: ${redact(error.message)}`));
    socket.on('open', () => {
      clearTimeout(timer);
      timer = setTimeout(() => finish('no-rpc-response'), expectsRpc ? 20_000 : 5_000);
      socket.send(JSON.stringify({ type: 'custom', event: 'nuxt:devtools:rpc', data: stringifyRpc({ t: 'q', m: 'getServerPages', a: [], i: id }) }));
    });
    socket.on('message', payload => {
      try {
        const envelope = JSON.parse(String(payload));
        if (envelope.event !== 'nuxt:devtools:rpc') return;
        const value = parseRpc(envelope.data);
        if (value.i === id && value.t === 's') finish(Array.isArray(value.r) && !value.e ? 'rpc-response' : 'rpc-error');
      } catch { /* Other Vite messages are unrelated to this request. */ }
    });
  });
}
const pending = new Map();
function frame(payload, received) {
  if (!parseRpc || typeof payload !== 'string') return;
  try {
    const envelope = JSON.parse(payload);
    if (envelope.type !== 'custom' || envelope.event !== 'nuxt:devtools:rpc') return;
    const value = parseRpc(envelope.data);
    if (!received && value.t === 'q') {
      pending.set(value.i, value.m);
      report.rpcRequests ||= [];
      if (!report.rpcRequests.includes(value.m)) report.rpcRequests.push(value.m);
      events.emit('rpc-request', value.m);
    } else if (received && pending.has(value.i)) {
      const method = pending.get(value.i);
      if (['getServerPages', 'getComponents'].includes(method)) {
        const result = { method, ok: !value.e, count: Array.isArray(value.r) ? value.r.length : null };
        if (method === 'getServerPages') result.hasIndex = Array.isArray(value.r) && value.r.some(page => page.path === '/');
        report.rpc.push(result);
        events.emit(method, result);
      }
    }
  } catch { /* Non-RPC HMR frames have a separate wire format. */ }
}
async function event(name, wait = 20_000) {
  const previous = report.rpc.find(result => result.method === name);
  if (previous) return previous;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => { events.off(name, receive); reject(new Error(`No ${name} RPC response`)); }, wait);
    function receive(value) { clearTimeout(timer); resolve(value); }
    events.once(name, receive);
  });
}

let context;
let browser;
let profile;
let page;
try {
  const engine = { chromium, firefox, webkit }[config.browser];
  assert(engine, 'unknown browser');
  const contextOptions = { ignoreHTTPSErrors: false };
  if (process.env.PROXY_COMPAT_AUTH) contextOptions.httpCredentials = JSON.parse(process.env.PROXY_COMPAT_AUTH);
  if (config.browser === 'firefox' && config.ca) {
    profile = await fs.mkdtemp(path.join(os.tmpdir(), 'dvt-firefox-'));
    execFileSync('certutil', ['-N', '--empty-password', '-d', `sql:${profile}`]);
    execFileSync('certutil', ['-A', '-d', `sql:${profile}`, '-n', 'devtools-fixture', '-t', 'C,,', '-i', config.ca]);
    context = await engine.launchPersistentContext(profile, { headless: true, ...contextOptions });
  } else {
    browser = await engine.launch({ headless: true });
    context = await browser.newContext(contextOptions);
  }
  // Newly committed tunnel URLs can precede DNS/edge readiness. Establish an
  // actual verified HTTP response from this client before the one browser run.
  const readinessDeadline = Date.now() + 60_000;
  let ready = false;
  while (Date.now() < readinessDeadline) {
    try {
      const response = await context.request.get(origin.href, { timeout: 5_000 });
      if (response.status() === 200) { ready = true; break; }
    } catch { /* DNS, edge startup and TLS are all bounded by this readiness gate. */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  assert(ready, 'public HTTP ingress did not become ready with verified TLS');
  traceEvent('public-ready');
  page = await context.newPage();
  page.setDefaultTimeout(timeout);
  page.on('console', message => { if (['error', 'warning'].includes(message.type())) report.console.push(redact(message.text())); });
  page.on('pageerror', error => report.console.push(redact(error.stack || error)));
  page.on('requestfailed', request => report.console.push(redact(`${request.method()} ${request.url()}: ${request.failure()?.errorText}`)));
  page.on('websocket', socket => {
    const socketUrl = new URL(socket.url());
    report.websocketUrls.push(redact(socketUrl.href));
    traceEvent('websocket-open', { url: redact(socketUrl.href) });
    socket.on('close', () => { report.closedSockets = (report.closedSockets || 0) + 1; traceEvent('websocket-close'); });
    if (socketUrl.host !== origin.host) report.directBackendFallback = true;
    if (config.case === 'nuxt') nuxtSocketUrl = socket.url();
    socket.on('framesent', ({ payload }) => { traceEvent('frame-sent', { bytes: Buffer.byteLength(payload) }); frame(payload, false); });
    socket.on('framereceived', ({ payload }) => { traceEvent('frame-received', { bytes: Buffer.byteLength(payload) }); frame(payload, true); });
  });
  const response = await page.goto(origin.href, { waitUntil: 'domcontentloaded', timeout });
  assert.equal(response.status(), 200, 'public page status');
  traceEvent('page-response', { status: response.status() });
  await page.getByTestId('marker').waitFor();

  if (['ws', 'socketio'].includes(config.case)) {
    if (config.case === 'ws') {
      report.headers = await page.evaluate(basic => fetch('/headers', { headers: { 'X-Forwarded-Host': 'spoof.invalid', 'X-Forwarded-Proto': 'spoof', Forwarded: 'host=spoof.invalid', ...(basic ? {} : { Authorization: 'Bearer fixture-application-canary' }) } }).then(response => response.json()), !!process.env.PROXY_COMPAT_AUTH);
      assert.equal(report.headers.host, config.policy === 'preserve' ? origin.host : config.backendAuthority);
      assert.equal(report.headers.forwardedHost, origin.host);
      assert.equal(report.headers.forwardedProto, origin.protocol.slice(0, -1));
      assert.equal(report.headers.authorizationPresent, !process.env.PROXY_COMPAT_AUTH);
      assert.equal(report.headers.forwardedPresent, false);
    }
    if (config.policy === 'rewrite') {
      await page.waitForFunction(() => window.__proxyProbe?.closed || window.__proxyProbe?.rejected, null, { timeout: 15_000 });
      assert.equal(await page.evaluate(() => !!window.__proxyProbe.reply), false, 'strict fixture accepted rewritten authority');
      report.application = 'expected-origin-rejection';
    } else {
      await page.waitForFunction(() => window.__proxyProbe?.reply === 'proxy-echo');
      if (config.case === 'socketio') {
        await page.waitForFunction(() => window.__proxyProbe.upgraded === true);
        report.pollingUpgrade = true;
        await page.goto(`${origin.origin}/?transport=websocket`, { waitUntil: 'domcontentloaded' });
        await page.waitForFunction(() => window.__proxyProbe?.reply === 'proxy-echo');
        await page.evaluate(() => { window.__proxyProbe.reply = null; window.__proxySocket.disconnect(); window.__proxySocket.connect(); });
        await page.waitForFunction(() => window.__proxyProbe.connectCount >= 2 && window.__proxyProbe.reply === 'proxy-echo');
        report.websocketFirstAndReconnect = true;
      }
      report.application = 'message-ack';
    }
  } else {
    if (['nuxt', 'next', 'sveltekit'].includes(config.case)) await page.waitForFunction(() => window.__compatReady === true);
    const documentId = await page.evaluate(() => { window.__compatDocument = `${Date.now()}-${Math.random()}`; return window.__compatDocument; });
    await page.getByTestId('counter').click();
    await page.getByTestId('counter').filter({ hasText: /^1$/ }).waitFor();
    const source = path.join(config.fixtureWork, config.hmrFile);
    const before = await fs.readFile(source, 'utf8');
    assert(before.includes('compat-before'), 'fixture HMR marker missing');
    await fs.writeFile(source, before.replace('compat-before', 'compat-after'));
    await page.getByTestId('marker').filter({ hasText: 'compat-after' }).waitFor();
    assert.equal(await page.evaluate(() => window.__compatDocument), documentId, 'full reload replaced HMR');
    assert.equal(await page.getByTestId('counter').innerText(), '1', 'HMR lost user state');
    report.hmr = 'updated-with-state';
    traceEvent('hmr-dom', { marker: 'compat-after', counter: '1', documentPreserved: true });

    if (config.case === 'nuxt') {
      // The independent Node client has its own resolver, separate from the
      // browser. Resolve once before testing guards; keep the public URL, Host,
      // SNI and certificate validation unchanged for every probe.
      assert.equal(new URL(nuxtSocketUrl).hostname, origin.hostname);
      const dnsDeadline = Date.now() + 60_000;
      while (!nuxtSocketAddress && Date.now() < dnsDeadline) {
        try { nuxtSocketAddress = await lookup(origin.hostname, { family: 4 }); }
        catch (error) {
          if (!['ENOTFOUND', 'EAI_AGAIN'].includes(error.code)) throw error;
          await new Promise(resolve => setTimeout(resolve, 100));
        }
      }
      assert(nuxtSocketAddress, 'RPC client DNS did not become ready');
      traceEvent('rpc-dns-ready');
      report.originProbes = [];
      for (const supplied of [undefined, origin.origin, `${origin.protocol}//different.localhost`, `${origin.protocol}//${origin.hostname}:1`, 'null']) {
        const allowed = supplied === undefined || (supplied === origin.origin && config.policy === 'preserve');
        const result = await probeNuxtOrigin(supplied, allowed);
        report.originProbes.push({ origin: supplied ?? '(absent)', result });
        assert.equal(result, allowed ? 'rpc-response' : 'no-rpc-response', `Nuxt origin guard ${supplied}: ${result}`);
      }
      await page.waitForFunction(() => !!window.__NUXT_DEVTOOLS_HOST__);
      await page.evaluate(() => window.__NUXT_DEVTOOLS_HOST__.devtools.navigate('/modules/pages'));
      const devtools = page.frameLocator('#nuxt-devtools-iframe');
      const start = devtools.getByRole('link', { name: 'Get Started' });
      await start.waitFor();
      await devtools.getByText('Send anonymous statistics, help us improving DevTools', { exact: true }).click();
      assert.equal(await devtools.getByRole('checkbox').isChecked(), false);
      await start.click();
      await page.evaluate(() => window.__NUXT_DEVTOOLS_HOST__.devtools.navigate('/modules/pages'));
      if (config.policy === 'preserve') {
        const pages = await event('getServerPages');
        assert(pages.ok && pages.hasIndex, 'DevTools pages RPC did not return the fixture route');
        await page.evaluate(() => window.__NUXT_DEVTOOLS_HOST__.devtools.navigate('/modules/components'));
        const components = await event('getComponents');
        assert(components.ok && components.count !== null, 'DevTools components RPC failed');
      } else {
        // The baseline HMR socket stays open but DevTools RPC is not registered.
        const result = await event('getServerPages', 5_000).then(() => 'unexpected-response', () => 'expected-no-rpc');
        assert.equal(result, 'expected-no-rpc');
        report.rpcBaseline = result;
      }
    }
    if (config.case === 'sveltekit') {
      await page.getByRole('link', { name: 'Second page' }).click();
      await page.getByRole('heading', { name: 'Second page' }).waitFor();
      await page.getByRole('link', { name: 'Home' }).click();
      const submitted = page.waitForResponse(response => response.request().method() === 'POST');
      await page.getByRole('button', { name: 'Submit' }).click();
      assert.equal((await submitted).status(), 200);
      report.navigationAndForm = true;
    }
  }
  // Engine.IO rejects the initial polling handshake before a WebSocket exists.
  if (config.case !== 'socketio' || config.policy !== 'rewrite') {
    assert(report.websocketUrls.length > 0, 'no application/HMR WebSocket observed');
  }
  assert(!report.directBackendFallback, 'browser connected directly to a backend');
  if (config.stopProbe) {
    const before = report.closedSockets || 0;
    await fs.writeFile(path.join(output, 'ready-to-stop'), 'ready');
    const deadline = Date.now() + 30_000;
    while (Date.now() < deadline) {
      if (await fs.stat(path.join(output, 'stop-done')).then(() => true, () => false)) break;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert(Date.now() < deadline, 'host did not stop the owned exposure');
    if (config.case !== 'nuxt') await page.waitForFunction(() => window.__proxyProbe?.closed === true, null, { timeout: 10_000 });
    const closeDeadline = Date.now() + 10_000;
    while ((report.closedSockets || 0) <= before && Date.now() < closeDeadline) await new Promise(resolve => setTimeout(resolve, 100));
    assert((report.closedSockets || 0) > before, 'stop left the application/HMR socket open');
    report.stopClosedSocket = true;
  }
  report.status = 'pass';
  report.browserVersion = browser?.version() || context.browser()?.version() || config.browser;
} catch (error) {
  report.status = 'fail';
  report.error = redact(error.stack || error);
  if (page) await page.screenshot({ path: path.join(output, 'failure.png'), fullPage: true }).catch(() => {});
} finally {
  await context?.close();
  await browser?.close();
  if (profile) await fs.rm(profile, { recursive: true, force: true });
  await fs.writeFile(path.join(output, 'browser-result.json'), JSON.stringify(report, null, 2) + '\n');
  // A bounded trace records only event kinds, byte counts and selected DOM/RPC
  // facts. Full Playwright traces would retain Vite tokens and DevTools payloads.
  await fs.writeFile(path.join(output, 'browser-trace.json'), JSON.stringify(trace, null, 2) + '\n');
}
console.log(JSON.stringify(report));
if (report.status !== 'pass') process.exitCode = 1;
