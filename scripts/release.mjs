#!/usr/bin/env node
import { execFileSync } from 'node:child_process';

const git = (...args) => execFileSync('git', args, { encoding: 'utf8' }).trim();

try {
  const args = process.argv.slice(2);
  if (args.some(arg => arg !== '--publish') || args.length > 1) {
    throw new Error('Usage: pnpm release [--publish]');
  }
  const publish = args.includes('--publish');
  const remote = git('remote').split('\n').includes('origin');
  if (remote) git('fetch', 'origin', '--tags');

  // Stable tags reachable from HEAD are the release baseline.
  const previous = git('tag', '--merged', 'HEAD', '--sort=-version:refname')
    .split('\n').find(tag => /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(tag));
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
  if (!remote) throw new Error('Configure origin before publishing.');
  if (git('branch', '--show-current') !== 'main') throw new Error('Publish from main.');
  if (git('status', '--porcelain')) throw new Error('Commit or stash working tree changes first.');
  if (git('tag', '--list', tag)) throw new Error(`${tag} already exists. Check its release before retrying.`);
  git('tag', '-a', tag, '-m', `Release ${tag}`);
  try {
    git('push', '--atomic', 'origin', 'refs/heads/main:refs/heads/main', `refs/tags/${tag}`);
  } catch {
    throw new Error(`Push was not confirmed. Local tag ${tag} remains; check origin, then retry: git push --atomic origin main refs/tags/${tag}`);
  }
  console.log(`\nPublished tag ${tag}. GitHub Actions will validate and publish the release.`);
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
