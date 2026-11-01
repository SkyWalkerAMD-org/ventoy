"""Legacy runner migration tests; all files/transports are isolated fixtures."""
import hashlib
import io
import json
import os
from pathlib import Path
import stat
import sys
import tarfile
import tempfile
import types
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts/ventoy-setup-upgrade.sh'
SOURCE = SCRIPT.read_text().split("<<'VENTOY_UPGRADE_PYTHON'\n", 1)[1].rsplit('\nVENTOY_UPGRADE_PYTHON', 1)[0]


def runner(version, modern=False):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w:gz') as archive:
        for name, raw in [('VERSION', version.encode()), ('software-client.json', b'{"enabled":true}')]:
            entry = tarfile.TarInfo('ventoy-manual-setup/' + name)
            entry.size = len(raw)
            archive.addfile(entry, io.BytesIO(raw))
    data = stream.getvalue()
    header = "#!/bin/bash\n# '%s'\n%s" % (hashlib.sha256(data).hexdigest(), '# --upgrade' if modern else '# legacy')
    return header.encode() + b'\n__VENTOY_MANUAL_ARCHIVE__\n' + data


class LegacyUpgrade(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='vc-legacy-upgrade-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve()
        self.target = self.root / 'ventoy-linux-setup.run'
        self.old = runner('0.3.0-alpha.47')
        self.new = runner('0.3.0-alpha.51', modern=True)
        self.target.write_bytes(self.old)
        self.target.chmod(0o600)
        self.sidecar = Path(str(self.target) + '.sha256')
        self.sidecar.write_text('obsolete checksum\n')
        self.sidecar.chmod(0o600)
        self.release = dict(version='0.3.0-alpha.51', bytes=len(self.new), sha256=hashlib.sha256(self.new).hexdigest())
        self.mod = types.ModuleType('legacy_test')
        exec(compile(SOURCE, str(SCRIPT), 'exec'), self.mod.__dict__)
        original_stat = os.stat
        def owned(path, *args, **kwargs):
            value = original_stat(path, *args, **kwargs)
            if str(path).startswith(str(self.root)):
                parts = list(value); parts[4] = 0
                return os.stat_result(parts)
            return value
        patch = mock.patch.object(self.mod.os, 'stat', side_effect=owned)
        patch.start(); self.addCleanup(patch.stop)
        self.calls = []
        self.before_download = lambda: None
        def get(endpoint):
            self.calls.append(endpoint)
            if endpoint == 'setup-release':
                return io.BytesIO(json.dumps(self.release).encode())
            self.before_download()
            return io.BytesIO(self.new)
        self.mod.transport = lambda files, ca: get

    def test_alpha47_nonexecutable_runner_upgrade_backup_checksum_and_repeat(self):
        self.mod.update(str(self.target))
        self.assertEqual(self.target.read_bytes(), self.new)
        self.assertEqual(stat.S_IMODE(self.target.stat().st_mode), 0o700)
        self.assertEqual(next(self.root.glob('*.previous-*')).read_bytes(), self.old)
        self.assertEqual(self.sidecar.read_text(), self.release['sha256'] + '  ventoy-linux-setup.run\n')
        self.target.chmod(0o600)
        self.mod.update(str(self.target))
        self.assertEqual(stat.S_IMODE(self.target.stat().st_mode), 0o700)
        self.assertEqual(len(list(self.root.glob('*.previous-*'))), 1)
        self.assertFalse(list(self.root.glob('.ventoy-*')))

    def test_check_only_preserves_runner_permissions_and_sidecar(self):
        self.mod.update(str(self.target), True)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertEqual(stat.S_IMODE(self.target.stat().st_mode), 0o600)
        self.assertEqual(self.sidecar.read_text(), 'obsolete checksum\n')
        self.assertEqual(self.calls, ['setup-release'])

    def test_bad_checksum_does_not_replace(self):
        self.release['sha256'] = 'a' * 64
        with self.assertRaisesRegex(ValueError, 'checksum'):
            self.mod.update(str(self.target))
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertFalse(list(self.root.glob('.ventoy-*')))

    def test_concurrently_changed_runner_is_preserved(self):
        self.before_download = lambda: self.target.write_bytes(b'operator changed')
        with self.assertRaisesRegex(ValueError, 'changed'):
            self.mod.update(str(self.target))
        self.assertEqual(self.target.read_bytes(), b'operator changed')

    def test_invalid_legacy_payload_does_not_contact_center(self):
        self.target.write_bytes(self.old.replace(b"'", b'!', 2))
        with self.assertRaisesRegex(ValueError, 'checksum'):
            self.mod.update(str(self.target))
        self.assertFalse(self.calls)

    @unittest.skipIf(sys.platform == 'win32', 'Linux path and mode behavior')
    def test_symlink_checksum_refused_without_overwriting_destination(self):
        self.sidecar.unlink()
        other = self.root / 'unrelated'; other.write_bytes(b'preserve')
        self.sidecar.symlink_to(other)
        with self.assertRaisesRegex(ValueError, 'symlink'):
            self.mod.update(str(self.target))
        self.assertEqual(other.read_bytes(), b'preserve')
        self.assertEqual(self.target.read_bytes(), self.old)


if __name__ == '__main__':
    if sys.platform != 'linux':
        print('SKIP: legacy setup upgrade uses Linux file modes')
    else:
        unittest.main()
