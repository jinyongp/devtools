"""Release trust and fallback contracts; no Actions token or native Windows needed."""
import copy
import os
import tempfile
from unittest.mock import patch
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('reuse', Path(__file__).with_name('reuse-windows-validation.py'))
reuse = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reuse)
SHA = 'a' * 40
REPO = 'owner/project'


def fixture():
    run = dict(id=123, run_attempt=1, head_sha=SHA, event='push', head_branch='main',
               path=reuse.WORKFLOW, status='completed', conclusion='success',
               repository={'full_name': REPO}, head_repository={'full_name': REPO})
    jobs = [dict(name=f'Windows {arch} / {lane}', status='completed', conclusion='success',
                 steps=[dict(name=name, status='completed', conclusion='success') for name in required])
            for arch in ('amd64', 'arm64') for lane, required in reuse.REQUIRED_STEPS.items()
            if not ((arch == 'arm64' and lane == 'race') or (arch == 'amd64' and lane == 'installer-x64'))]
    return run, jobs


class TrustTests(unittest.TestCase):
    def find(self, run=None, jobs=None, latest=None):
        good_run, good_jobs = fixture()
        run = good_run if run is None else run
        jobs = good_jobs if jobs is None else jobs
        def api(path):
            if '/workflows/' in path:
                return {'workflow_runs': [run]}
            if '/jobs?' in path:
                return {'jobs': jobs}
            return run if latest is None else latest
        return reuse.find_validation(api, REPO, SHA, SHA)

    def test_exact_complete_main_push_is_reused(self):
        self.assertEqual(self.find()['id'], 123)

    def test_untrusted_or_incomplete_runs_are_rejected(self):
        for key, value in dict(head_sha='b' * 40, event='pull_request', head_branch='topic',
                               path='other.yml', status='in_progress', conclusion='failure',
                               repository={'full_name': 'fork/project'},
                               head_repository={'full_name': 'fork/project'}).items():
            with self.subTest(key=key):
                run, _ = fixture()
                run[key] = value
                self.assertIsNone(self.find(run=run))

    def test_missing_failed_or_skipped_jobs_and_steps_are_rejected(self):
        _, jobs = fixture()
        self.assertIsNone(self.find(jobs=jobs[:-1]))
        for state in ('failure', 'skipped', 'cancelled'):
            with self.subTest(state=state):
                changed = copy.deepcopy(jobs)
                changed[0]['conclusion'] = state
                self.assertIsNone(self.find(jobs=changed))
                changed = copy.deepcopy(jobs)
                changed[0]['steps'][0]['conclusion'] = state
                self.assertIsNone(self.find(jobs=changed))

    def test_every_windows_suite_shard_is_required_on_both_architectures(self):
        _, jobs = fixture()
        for arch in ('amd64', 'arm64'):
            for lane in ('cli', 'values', 'tasks', 'handoff', 'runtime', 'rest'):
                name = f'Windows {arch} / suite-{lane}'
                with self.subTest(job=name):
                    self.assertIn(name, {job['name'] for job in jobs})
                    self.assertIsNone(self.find(jobs=[job for job in jobs if job['name'] != name]))

    def test_rerun_in_progress_invalidates_old_success(self):
        run, _ = fixture()
        run.update(status='in_progress', conclusion=None, run_attempt=2)
        self.assertIsNone(self.find(latest=run))

    def test_different_workflow_sha_or_forced_full_never_queries_evidence(self):
        def unexpected_api(_):
            self.fail('must rerun source checks without consulting another workflow revision')
        self.assertIsNone(reuse.find_validation(unexpected_api, REPO, SHA, 'b' * 40))
        self.assertIsNone(reuse.find_validation(unexpected_api, REPO, SHA, SHA, False))

    def test_api_outage_or_malformed_evidence_runs_full_checks(self):
        for error in (OSError('offline'), ValueError('invalid JSON'), KeyError('missing field')):
            with self.subTest(error=type(error).__name__), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                env = dict(GITHUB_REPOSITORY=REPO, SOURCE_COMMIT=SHA, GITHUB_SHA=SHA,
                           REUSE_VALIDATION='true', GITHUB_OUTPUT=str(root / 'output'),
                           GITHUB_STEP_SUMMARY=str(root / 'summary'))
                with patch.dict(os.environ, env), patch.object(reuse, 'github_api', side_effect=error):
                    reuse.main()
                self.assertEqual((root / 'output').read_text(), 'windows-source-reused=false\n')
                self.assertIn('all native checks run', (root / 'summary').read_text())

    def test_no_successful_run_falls_back(self):
        self.assertIsNone(reuse.find_validation(lambda _: {'workflow_runs': []}, REPO, SHA, SHA))


if __name__ == '__main__':
    unittest.main()
