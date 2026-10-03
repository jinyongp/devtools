import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const script = fileURLToPath(new URL('./project.sh', import.meta.url));

test('aggregate checks use the checkout CLI and stop after a failed gate', t => {
  const root = mkdtempSync(join(tmpdir(), 'devtools-project-check-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, 'bin'));
  const calls = join(root, 'calls');
  const runner = join(root, 'bin', 'devtools');
  writeFileSync(runner, '#!/bin/sh\nprintf "%s\\n" "$*" >> "$PROJECT_TEST_CALLS"\nexit 23\n');
  chmodSync(runner, 0o755);
  const go = join(root, 'go');
  writeFileSync(go, '#!/bin/sh\ntouch "$PROJECT_TEST_GO"\nexit 99\n');
  chmodSync(go, 0o755);
  const goCalled = join(root, 'go-called');
  const result = spawnSync('sh', [script, 'check'], { cwd: root, encoding: 'utf8',
    env: { ...process.env, PATH: root + ':' + process.env.PATH,
      PROJECT_TEST_CALLS: calls, PROJECT_TEST_GO: goCalled } });
  assert.equal(result.status, 23, result.stderr);
  assert.equal(readFileSync(calls, 'utf8'), 'run check:skill-release\n');
  assert.equal(existsSync(goCalled), false);
});

test('package commands require one version and preserve explicit environment inputs', t => {
  const root = mkdtempSync(join(tmpdir(), 'devtools-project-package-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, 'scripts'));
  const capture = join(root, 'capture');
  writeFileSync(join(root, 'scripts', 'package.sh'), 'printf "%s\\n" "$VERSION" "$OUTPUT_DIR" "$TARGET_OS" > "$PROJECT_TEST_CAPTURE"\n');
  const env = { ...process.env, VERSION: 'inherited', OUTPUT_DIR: 'output with spaces',
    TARGET_OS: 'linux', PROJECT_TEST_CAPTURE: capture };
  for (const args of [[], ['0.0.0-test', 'extra']]) {
    const result = spawnSync('sh', [script, 'package-cli', ...args], { cwd: root, env, encoding: 'utf8' });
    assert.equal(result.status, 2);
    assert.equal(existsSync(capture), false);
  }
  const result = spawnSync('sh', [script, 'package-cli', '0.0.0-test'], { cwd: root, env, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(readFileSync(capture, 'utf8'), '0.0.0-test\noutput with spaces\nlinux\n');
});
