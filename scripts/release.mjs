#!/usr/bin/env node
import { execFileSync } from 'node:child_process';

const run = (program, ...args) => execFileSync(program, args, { encoding: 'utf8' }).trim();
const git = (...args) => run('git', ...args);
const gh = (...args) => run('gh', ...args);
const stableVersion = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;

try {
  const args = process.argv.slice(2);
  if (args.some(arg => arg !== '--publish') || args.length > 1) {
    throw new Error('Usage: pnpm release [--publish]');
  }
  const publish = args.includes('--publish');
  if (!git('remote').split('\n').includes('origin')) throw new Error('Configure origin before planning a release.');
  git('fetch', 'origin', '--tags');

  // Failed or unpublished tags never advance the released version.
  const reachable = new Set(git('tag', '--merged', 'HEAD').split('\n'));
  const releases = JSON.parse(gh('api', 'repos/{owner}/{repo}/releases?per_page=100', '--paginate', '--slurp')).flat();
  const published = new Set(releases.filter(release =>
    !release.draft && !release.prerelease && release.published_at && stableVersion.test(release.tag_name)
  ).map(release => release.tag_name));
  const previous = git('tag', '--sort=-version:refname').split('\n')
    .find(tag => reachable.has(tag) && published.has(tag));
  const range = previous ? `${previous}..HEAD` : 'HEAD';
  const messages = git('log', '--format=%B%x00', range).split('\0').map(s => s.trim()).filter(Boolean);
  if (!messages.length) {
    console.log(`No commits since ${previous}.`);
    process.exit(0);
  }
  const breaking = messages.some(m => /^[a-z]+(?:\([^\n]+\))?!:/i.test(m) || /^BREAKING[ -]CHANGE:/m.test(m));
  const feature = messages.some(m => /^feat(?:\([^\n]+\))?:/i.test(m));
  const bump = breaking || feature ? 'minor' : 'patch';
  let version = '0.1.0';
  if (previous) {
    let [major, minor, patch] = previous.slice(1).split('.').map(Number);
    if (major !== 0) throw new Error('This release command currently supports 0.x releases.');
    if (bump === 'minor') { minor++; patch = 0; }
    else patch++;
    version = `${major}.${minor}.${patch}`;
  }
  const tag = `v${version}`;
  console.log(`Previous: ${previous ?? 'none'}\nRecommended: ${tag} (${previous ? bump : 'first release'})\n`);
  console.log(git('log', '--oneline', range));
  if (!publish) {
    console.log('\nPublish: pnpm release --publish');
    process.exit(0);
  }
  if (git('branch', '--show-current') !== 'main') throw new Error('Publish from main.');
  if (git('status', '--porcelain')) throw new Error('Commit or stash working tree changes first.');
  const commit = git('rev-parse', 'HEAD');
  if (git('tag', '--list', tag) && git('rev-parse', `${tag}^{commit}`) !== commit) {
    throw new Error(`${tag} belongs to another commit but has no published stable release. Resolve that tag before publishing; it does not advance the recommended version.`);
  }
  git('push', 'origin', 'refs/heads/main:refs/heads/main');
  const dispatched = gh('workflow', 'run', 'release.yml', '--ref', 'main',
    '-f', `version=${version}`, '-f', `commit=${commit}`, '-f', 'publish=true');
  console.log(`\nRequested ${tag} at ${commit}. GitHub Actions creates the tag only after validation and packaging succeed.`);
  if (dispatched) console.log(dispatched);
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
