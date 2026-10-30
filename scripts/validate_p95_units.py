"""Render both systemd unit variants without installing anything (Linux CI)."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(os.name == 'posix', 'Linux shell fixtures')
class UnitCompatibilityTests(unittest.TestCase):
    def render(self, version, product='fyhocsys'):
        with tempfile.TemporaryDirectory() as folder:
            fake = Path(folder) / 'systemctl'
            fake.write_text('#!/bin/sh\n[ "$1" = --version ] || exit 90\nprintf "systemd %s\\n"\n' % version)
            fake.chmod(0o700)
            return subprocess.run(['bash', str(ROOT/'tools/p95-priority-helper/install.sh'), '--print-unit', product],
                                  env=dict(os.environ, PATH=folder+os.pathsep+os.environ['PATH']),
                                  capture_output=True, text=True)

    def test_219_retains_crash_undo_without_unsupported_keys(self):
        for product in ('fyhocsys', 'sckocstp'):
            result = self.render(219, product)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn('RuntimeDirectory', result.stdout)
            self.assertIn('Type=notify', result.stdout)
            self.assertIn('CapabilityBoundingSet=CAP_SYS_NICE CAP_SYS_PTRACE', result.stdout)
            self.assertIn('ExecStart=/usr/local/sbin/p95-priority-'+product+' watch --product '+product, result.stdout)

    def test_modern_unit_preserves_runtime_journal(self):
        result = self.render(235)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('RuntimeDirectory=p95-priority-fyhocsys', result.stdout)
        self.assertIn('RuntimeDirectoryPreserve=yes', result.stdout)

    def test_too_old_systemd_rejected(self):
        result = self.render(218)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('[Service]', result.stdout)


if __name__ == '__main__':
    unittest.main()
