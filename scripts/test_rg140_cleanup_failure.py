import importlib.util
import pathlib
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('fault', pathlib.Path(__file__).with_name('rg140-cleanup-failure.py'))
fault = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fault)
VERSION = '11111111-1111-4111-8111-111111111111'

class FaultTests(unittest.TestCase):
    def run_helper(self, *, terminal='failed', drift=False, timer_error=False, interrupted=False):
        with tempfile.TemporaryDirectory() as root:
            initial = dict(id=VERSION, state='client_refresh_pending', cutover=True,
                           placement=fault.US, cleanup=None, job=None)
            cleaning = dict(initial, state='target_cleaning', placement=fault.FI,
                            cleanup=VERSION, job=VERSION)
            pending = dict(status='pending', expected_node=True, error='')
            result = dict(status=terminal, expected_node=True,
                          error='write staged config: is a directory')
            results = [pending, pending] if interrupted else [pending, result]
            baseline = ({'protected': 'same'}, {'sing-box': 'same PID'})
            snapshots = [baseline, ({'protected': 'changed'}, baseline[1]) if drift else baseline]
            with patch.object(fault, 'STAGING', pathlib.Path(root)), \
                 patch.object(pathlib.Path, 'read_text', return_value=''), \
                 patch.object(fault, 'operation', side_effect=[initial, cleaning]), \
                 patch.object(fault, 'only_canary_job'), \
                 patch.object(fault, 'job_result', side_effect=results), \
                 patch.object(fault, 'snapshot', side_effect=snapshots), \
                 patch.object(fault, 'command', side_effect=fault.Stop('timer failed') if timer_error else None), \
                 patch.object(fault.subprocess, 'run'), \
                 patch.object(fault.time, 'sleep', side_effect=fault.Stop('interrupted') if interrupted else None):
                error = None
                try:
                    fault.run()
                except fault.Stop as caught:
                    error = str(caught)
            self.assertFalse((pathlib.Path(root)/(VERSION+'.json.tmp')).exists())
            return error

    def test_real_failed_job_and_unchanged_baseline_succeed(self):
        self.assertIsNone(self.run_helper())

    def test_agent_wins_race_fault_removed_without_acceptance(self):
        self.assertIn('failure_not_observed', self.run_helper(terminal='succeeded'))

    def test_timer_failure_restores_fault(self):
        self.assertEqual(self.run_helper(timer_error=True), 'timer failed')

    def test_interrupted_wait_restores_fault(self):
        self.assertEqual(self.run_helper(interrupted=True), 'interrupted')

    def test_protected_drift_restores_fault_but_refuses_acceptance(self):
        self.assertIn('protected_files', self.run_helper(drift=True))

    def test_real_write_fails_and_exact_empty_fault_is_removed(self):
        with tempfile.TemporaryDirectory() as root, patch.object(fault, 'STAGING', pathlib.Path(root)):
            path, inode = fault.install_fault(VERSION)
            with self.assertRaises(IsADirectoryError):
                path.write_bytes(b'candidate')
            sibling = pathlib.Path(root) / 'unrelated.json'
            sibling.write_bytes(b'unchanged')
            fault.remove_fault(path, inode)
            self.assertFalse(path.exists())
            self.assertEqual(sibling.read_bytes(), b'unchanged')

    def test_existing_candidate_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as root, patch.object(fault, 'STAGING', pathlib.Path(root)):
            path = pathlib.Path(root) / (VERSION + '.json.tmp')
            path.write_bytes(b'existing')
            with self.assertRaisesRegex(fault.Stop, 'already_staged'):
                fault.install_fault(VERSION)
            self.assertEqual(path.read_bytes(), b'existing')

    def test_symlink_root_is_refused(self):
        with tempfile.TemporaryDirectory() as root:
            target = pathlib.Path(root) / 'target'
            target.mkdir()
            link = pathlib.Path(root) / 'link'
            link.symlink_to(target)
            with patch.object(fault, 'STAGING', link), self.assertRaisesRegex(fault.Stop, 'symlink'):
                fault.install_fault(VERSION)

    def test_path_traversal_is_refused(self):
        with self.assertRaisesRegex(fault.Stop, 'invalid_version'):
            fault.install_fault('../anything')

    def test_replaced_fault_is_not_removed(self):
        with tempfile.TemporaryDirectory() as root, patch.object(fault, 'STAGING', pathlib.Path(root)):
            path, inode = fault.install_fault(VERSION)
            path.rmdir()
            path.write_bytes(b'new owner')
            with self.assertRaisesRegex(fault.Stop, 'identity_changed'):
                fault.remove_fault(path, inode)
            self.assertEqual(path.read_bytes(), b'new owner')

    def test_nonempty_fault_is_not_recursively_removed(self):
        with tempfile.TemporaryDirectory() as root, patch.object(fault, 'STAGING', pathlib.Path(root)):
            path, inode = fault.install_fault(VERSION)
            (path / 'unexpected').write_bytes(b'keep')
            with self.assertRaises(OSError):
                fault.remove_fault(path, inode)
            self.assertTrue((path / 'unexpected').exists())

    def test_other_pending_work_refuses_fault(self):
        with patch.object(fault, 'sql', return_value='f'), self.assertRaisesRegex(fault.Stop, 'other_work'):
            fault.only_canary_job(VERSION)

if __name__ == '__main__':
    unittest.main()
