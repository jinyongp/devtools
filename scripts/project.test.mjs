import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { delimiter, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test, { after } from 'node:test';

const script = fileURLToPath(new URL('./project.py', import.meta.url));
const repository = fileURLToPath(new URL('../', import.meta.url));
const python = process.platform === 'win32' ? 'python' : 'python3';
const suffix = process.platform === 'win32' ? '.exe' : '';
let helper;
let helperRoot;
after(() => { if (helperRoot) rmSync(helperRoot, { recursive: true, force: true }); });
function gateHelper() {
  if (helper) return helper;
  helperRoot = mkdtempSync(join(tmpdir(), 'devtools-project-helper-'));
  const source = join(helperRoot, 'main.go');
  helper = join(helperRoot, 'gate' + suffix);
  writeFileSync(source, `package main
import("os";"path/filepath";"strings")
func main(){
 if strings.TrimSuffix(filepath.Base(os.Args[0]),".exe")=="devtools"{
  f,e:=os.OpenFile(os.Getenv("PROJECT_TEST_CALLS"),os.O_APPEND|os.O_CREATE|os.O_WRONLY,0600);if e!=nil{panic(e)};_,_=f.WriteString(strings.Join(os.Args[1:]," ")+"\\n");_=f.Close();os.Exit(23)
 };_=os.WriteFile(os.Getenv("PROJECT_TEST_GO"),[]byte("called"),0600);os.Exit(99)
}`);
  execFileSync('go', ['build', '-o', helper, source], { encoding: 'utf8' });
  return helper;
}

test('aggregate checks use the checkout CLI and stop after a failed gate', t => {
  const root = mkdtempSync(join(tmpdir(), 'devtools-project-check-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, 'bin'));
  const calls = join(root, 'calls');
  const runner = join(root, 'bin', 'devtools' + suffix);
  copyFileSync(gateHelper(), runner);
  chmodSync(runner, 0o755);
  const go = join(root, 'go' + suffix);
  copyFileSync(gateHelper(), go);
  chmodSync(go, 0o755);
  const goCalled = join(root, 'go-called');
  const result = spawnSync(python, ['-B', script, 'check'], { cwd: root, encoding: 'utf8',
    env: { ...process.env, PATH: root + delimiter + process.env.PATH,
      PROJECT_TEST_CALLS: calls, PROJECT_TEST_GO: goCalled } });
  assert.equal(result.status, 23, result.stderr);
  assert.equal(readFileSync(calls, 'utf8'), 'run check:skill-release\n');
  assert.equal(existsSync(goCalled), false);
});

test('package commands require one version and preserve explicit environment inputs', t => {
  const root = mkdtempSync(join(tmpdir(), 'devtools-project-package-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const output = join(root, 'output with spaces');
  const env = { ...process.env, VERSION: 'inherited', OUTPUT_DIR: output,
    TARGET_OS: 'linux', TARGET_ARCH: 'amd64', COMMIT: 'project-test' };
  for (const args of [[], ['0.0.0-test', 'extra']]) {
    const result = spawnSync(python, ['-B', script, 'package-cli', ...args], { cwd: repository, env, encoding: 'utf8' });
    assert.equal(result.status, 2, result.stderr);
    assert.equal(existsSync(output), false);
  }
  const result = spawnSync(python, ['-B', script, 'package-cli', '0.0.0-test'], { cwd: repository, env, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(existsSync(join(output, 'devtools_0.0.0-test_linux_amd64.tar.gz')), true);
  assert.equal(existsSync(join(output, 'devtools_0.0.0-test_linux_amd64.tar.gz.sha256')), true);
});

test('native compatibility entry forwards Python exit status', () => {
  const script = fileURLToPath(new URL(process.platform === 'win32' ? './project.cmd' : './project.sh', import.meta.url));
  const result = process.platform === 'win32'
    ? spawnSync('cmd.exe', ['/d', '/e:on', '/v:off', '/s', '/c', `""${script}" unknown-action"`], { encoding: 'utf8', windowsVerbatimArguments: true })
    : spawnSync('sh', [script, 'unknown-action'], { encoding: 'utf8' });
  assert.equal(result.status, 2, result.stderr);
  assert.match(result.stderr, /Unknown project action: unknown-action/);
});
