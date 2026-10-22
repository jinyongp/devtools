"""Reuse only a complete, successful main push for the release's exact workflow SHA.

Missing, inaccessible or unsuitable evidence means running all native checks again.
No binaries or artifacts from another run are promoted by this decision.
"""
import json
import os
from pathlib import Path
import re
import urllib.parse
import urllib.request

WORKFLOW = '.github/workflows/windows-platform.yml'
REQUIRED_STEPS = {
    'suite-cli': {'Complete native Go product suite'},
    'suite-values': {'Complete native Go product suite'},
    'suite-tasks': {'Complete native Go product suite'},
    'suite-handoff': {'Complete native Go product suite'},
    'suite-runtime': {'Prepare actual package-manager shims', 'Complete native Go product suite'},
    'suite-rest': {'Complete native Go product suite'},
    'boundaries': {'Verify handle cleanup negative control', 'Test opt-in large storage recovery',
                   'Repeat native project lifecycle', 'Repeat native TLS-only startup'},
    'race': {'Native amd64 race detector'},
    'product': {'Test portable project and release tooling with native packages',
                'Verify installed candidate and actual Chromium dashboard',
                'Test public native CLI and PowerShell completion'},
    'installer-ps5': {'Test selected native PowerShell installer and running-image updates'},
    'installer-ps7': {'Test native installer recovery', 'Test selected native PowerShell installer and running-image updates'},
    'installer-x64': {'Test selected native PowerShell installer and running-image updates'},
}


def trusted_run(run, repository, commit):
    return (run.get('head_sha') == commit and run.get('event') == 'push'
            and run.get('head_branch') == 'main' and run.get('path') == WORKFLOW
            and run.get('status') == 'completed' and run.get('conclusion') == 'success'
            and run.get('repository', {}).get('full_name') == repository
            and run.get('head_repository', {}).get('full_name') == repository)


def complete_jobs(jobs):
    by_name = {job['name']: job for job in jobs}
    for arch in ('amd64', 'arm64'):
        for lane, required in REQUIRED_STEPS.items():
            if (arch == 'arm64' and lane == 'race') or (arch == 'amd64' and lane == 'installer-x64'):
                continue
            job = by_name.get(f'Windows {arch} / {lane}', {})
            if job.get('status') != 'completed' or job.get('conclusion') != 'success':
                return False
            passed = {step['name'] for step in job.get('steps', [])
                      if step.get('status') == 'completed' and step.get('conclusion') == 'success'}
            if not required <= passed:
                return False
    return True


def find_validation(api, repository, commit, workflow_commit, enabled=True):
    if not enabled or commit != workflow_commit or not re.fullmatch('[0-9a-f]{40}', commit):
        return None
    query = urllib.parse.urlencode(dict(branch='main', event='push', head_sha=commit,
                                       status='success', per_page=10))
    runs = api(f'/repos/{repository}/actions/workflows/windows-platform.yml/runs?{query}')
    for run in runs.get('workflow_runs', []):
        if not trusted_run(run, repository, commit):
            continue
        # Fetch the current attempt, rather than trusting an outdated list response.
        latest = api(f'/repos/{repository}/actions/runs/{run["id"]}')
        if not trusted_run(latest, repository, commit):
            continue
        jobs = api(f'/repos/{repository}/actions/runs/{run["id"]}/jobs?filter=latest&per_page=100')
        if complete_jobs(jobs.get('jobs', [])):
            return latest
    return None


def github_api(path):
    request = urllib.request.Request(os.environ['GITHUB_API_URL'] + path, headers={
        'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
        'Accept': 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28',
    })
    with urllib.request.urlopen(request, timeout=15) as response:
        return json.load(response)


def main():
    run = None
    try:
        run = find_validation(github_api, os.environ['GITHUB_REPOSITORY'],
                              os.environ['SOURCE_COMMIT'], os.environ['GITHUB_SHA'],
                              os.environ['REUSE_VALIDATION'] == 'true')
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        print('Validation evidence unavailable; running all Windows checks.')
    reused = 'true' if run else 'false'
    with Path(os.environ['GITHUB_OUTPUT']).open('a') as output:
        output.write(f'windows-source-reused={reused}\n')
    with Path(os.environ['GITHUB_STEP_SUMMARY']).open('a') as summary:
        if run:
            # Construct a URL from repository/run identity, rather than an untrusted display field.
            url = f'{os.environ["GITHUB_SERVER_URL"]}/{os.environ["GITHUB_REPOSITORY"]}/actions/runs/{int(run["id"])}'
            summary.write(f'Windows source checks: [{os.environ["SOURCE_COMMIT"]}]({url}), '
                          f'attempt {int(run["run_attempt"])}. Final-version installation and product checks run again.\n')
        else:
            summary.write('No reusable Windows source validation; all native checks run.\n')
    print(f'Reuse Windows source validation: {reused}')


if __name__ == '__main__':
    main()
