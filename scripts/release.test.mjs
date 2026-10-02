import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const releaseScript = fileURLToPath(new URL('./release.mjs', import.meta.url));
const tagScript = fileURLToPath(new URL('./publish-release-tag.sh', import.meta.url));

function fixture(t, inherited = {}) {
  const root = mkdtempSync(join(tmpdir(), 'devtools-release-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const remote = join(root, 'remote.git');
  const repository = join(root, 'repository');
  const calls = join(root, 'gh-calls.jsonl');
  const env = { ...process.env, ...inherited, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_NOSYSTEM: '1',
    GIT_AUTHOR_NAME: 'Release Test', GIT_AUTHOR_EMAIL: 'release@example.test',
    GIT_COMMITTER_NAME: 'Release Test', GIT_COMMITTER_EMAIL: 'release@example.test',
    GH_CALLS: calls, RELEASES_JSON: JSON.stringify([[{
      tag_name: 'v0.22.3', draft: false, prerelease: false, published_at: '2026-10-02T00:00:00Z',
    }]]), PATH: root + ':' + process.env.PATH };
  for (const key of ['GIT_ALTERNATE_OBJECT_DIRECTORIES', 'GIT_CONFIG', 'GIT_CONFIG_PARAMETERS',
    'GIT_CONFIG_COUNT', 'GIT_OBJECT_DIRECTORY', 'GIT_DIR', 'GIT_WORK_TREE', 'GIT_IMPLICIT_WORK_TREE',
    'GIT_GRAFT_FILE', 'GIT_INDEX_FILE', 'GIT_NO_REPLACE_OBJECTS', 'GIT_REPLACE_REF_BASE',
    'GIT_PREFIX', 'GIT_SHALLOW_FILE', 'GIT_COMMON_DIR', 'GIT_NAMESPACE']) delete env[key];
  const command = (cwd, ...args) => execFileSync('git', args,
    { cwd, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  command(root, 'init', '--bare', '--initial-branch=main', remote);
  command(root, 'clone', remote, repository);
  const git = (...args) => command(repository, ...args);
  const commit = message => {
    writeFileSync(join(repository, 'content'), message);
    git('add', 'content');
    git('commit', '-m', message);
    return git('rev-parse', 'HEAD');
  };
  commit('docs: stable baseline');
  git('tag', 'v0.22.3');
  git('push', 'origin', 'main', 'v0.22.3');
  const feature = commit('feat(cli)!: change run argument boundaries');
  git('tag', 'v0.99.0'); // A failed/unpublished release must not become the baseline.
  git('push', 'origin', 'main', 'v0.99.0');
  const head = commit('fix(cli): preserve completion quoting');
  writeFileSync(join(root, 'gh'), `#!/usr/bin/env node
import { appendFileSync } from 'node:fs';
const args = process.argv.slice(2);
appendFileSync(process.env.GH_CALLS, JSON.stringify(args) + '\\n');
if (args[0] === 'api') {
  if (process.env.GH_API_FAIL) process.exit(1);
  console.log(process.env.RELEASES_JSON);
} else if (args[0] === 'workflow' && args[1] === 'run') {
  if (process.env.GH_DISPATCH_FAIL) process.exit(1);
  console.log('https://github.com/example/test/actions/runs/1');
} else process.exit(2);
`);
  chmodSync(join(root, 'gh'), 0o755);
  const run = (args = [], extra = {}) => spawnSync(process.execPath, [releaseScript, ...args],
    { cwd: repository, env: { ...env, ...extra }, encoding: 'utf8' });
  const publishTag = (extra = {}) => spawnSync('sh', [tagScript],
    { cwd: repository, env: { ...env, TAG: 'v0.23.0', COMMIT: git('rev-parse', 'HEAD'), ...extra }, encoding: 'utf8' });
  const ghCalls = () => readFileSync(calls, 'utf8').trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
  return { git, commit, feature, head, env, run, publishTag, ghCalls, remote, command, repository, root };
}

test('fixture setup cannot mutate an inherited caller repository or index', t => {
  const caller = fixture(t);
  const index = join(caller.repository, '.git', 'index');
  const before = readFileSync(index);
  const config = readFileSync(join(caller.repository, '.git', 'config'));
  const head = caller.git('rev-parse', 'HEAD');
  const inner = fixture(t, { GIT_DIR: join(caller.repository, '.git'),
    GIT_WORK_TREE: caller.repository, GIT_INDEX_FILE: index });
  assert.equal(caller.git('rev-parse', 'HEAD'), head);
  assert.deepEqual(readFileSync(index), before);
  assert.deepEqual(readFileSync(join(caller.repository, '.git', 'config')), config);
  assert.equal(inner.git('rev-parse', '--show-toplevel'), inner.repository);
});

test('version baseline uses published stable releases, excluding failed tags and drafts', t => {
  const f = fixture(t);
  f.git('tag', 'v0.24.0');
  f.git('tag', 'v0.25.0');
  const releases = JSON.parse(f.env.RELEASES_JSON);
  releases.push([
    { tag_name: 'v0.24.0', draft: true, prerelease: false, published_at: null },
    { tag_name: 'v0.25.0', draft: false, prerelease: true, published_at: '2026-10-02T00:00:00Z' },
  ]);
  const result = f.run([], { RELEASES_JSON: JSON.stringify(releases) });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Previous: v0.22.3\nRecommended: v0.23.0/);
  assert.equal(f.git('tag', '--list', 'v0.23.0'), '');
});

test('publish dispatches the exact commit and version without creating a tag', t => {
  const f = fixture(t);
  const result = f.run(['--publish']);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(f.ghCalls().at(-1), ['workflow', 'run', 'release.yml', '--ref', 'main',
    '-f', 'version=0.23.0', '-f', `commit=${f.head}`, '-f', 'publish=true']);
  assert.equal(f.command(f.remote, 'rev-parse', 'main'), f.head);
  assert.equal(f.git('tag', '--list', 'v0.23.0'), '');
  assert.equal(f.git('ls-remote', 'origin', 'refs/tags/v0.23.0'), '');
});

test('failed dispatch can retry the same version without consuming a tag', t => {
  const f = fixture(t);
  const failed = f.run(['--publish'], { GH_DISPATCH_FAIL: '1' });
  assert.equal(failed.status, 1);
  assert.equal(f.git('tag', '--list', 'v0.23.0'), '');
  const retried = f.run(['--publish']);
  assert.equal(retried.status, 0, retried.stderr);
  assert.match(retried.stdout, /Recommended: v0.23.0/);
  assert.equal(f.git('ls-remote', 'origin', 'refs/tags/v0.23.0'), '');
});

test('unpublished conflicting tag is reported without advancing or rewriting it', t => {
  const f = fixture(t);
  f.git('tag', 'v0.23.0', f.feature);
  f.git('push', 'origin', 'v0.23.0');
  const result = f.run(['--publish']);
  assert.equal(result.status, 1);
  assert.match(result.stdout, /Recommended: v0.23.0/);
  assert.match(result.stderr, /belongs to another commit/);
  assert.equal(f.git('rev-parse', 'v0.23.0'), f.feature);
  assert.equal(f.ghCalls().some(args => args[0] === 'workflow'), false);
});

test('release API failure fails planning without falling back to tags', t => {
  const f = fixture(t);
  const result = f.run(['--publish'], { GH_API_FAIL: '1' });
  assert.equal(result.status, 1);
  assert.equal(f.git('tag', '--list', 'v0.23.0'), '');
  assert.equal(f.ghCalls().some(args => args[0] === 'workflow'), false);
});

test('publication creates the tag once, reuses it on retry, and rejects a different commit', t => {
  const f = fixture(t);
  f.git('push', 'origin', 'main');
  const published = f.publishTag();
  assert.equal(published.status, 0, published.stderr);
  const tagObject = f.command(f.remote, 'rev-parse', 'v0.23.0');
  assert.equal(f.command(f.remote, 'rev-parse', 'v0.23.0^{}'), f.head);
  const retried = f.publishTag();
  assert.equal(retried.status, 0, retried.stderr);
  assert.match(retried.stdout, /Reusing v0.23.0/);
  assert.equal(f.command(f.remote, 'rev-parse', 'v0.23.0'), tagObject);
  f.commit('fix: another commit');
  f.git('push', 'origin', 'main');
  const conflicting = f.publishTag();
  assert.equal(conflicting.status, 1);
  assert.match(conflicting.stderr, /refusing to replace/);
  assert.equal(f.command(f.remote, 'rev-parse', 'v0.23.0'), tagObject);
});

test('publication rejects a commit outside main without creating a tag', t => {
  const f = fixture(t);
  f.git('switch', '-c', 'other');
  f.commit('feat: off-main');
  const result = f.publishTag();
  assert.equal(result.status, 1);
  assert.equal(f.git('tag', '--list', 'v0.23.0'), '');
  assert.equal(f.git('ls-remote', 'origin', 'refs/tags/v0.23.0'), '');
});
