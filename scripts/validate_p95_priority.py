"""P95 stays an independent tools file: copy/update/check without executing it."""
import base64
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import re
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'internal/center/assets/manual'))
import software


class PriorityStandaloneTests(unittest.TestCase):
    def test_standalone_payload_integrity_and_version(self):
        # Check the actual distributed file without starting its installer.
        script = (ROOT/'tools/p95-priority-setup.sh').read_bytes()
        header, payload = script.split(b'\n__P95_PAYLOAD_BELOW__\n', 1)
        archive = base64.b64decode(payload)
        def pin(name):
            return re.search(rb"^"+name+rb"='([^']+)'$", header, re.M).group(1).decode()
        self.assertEqual(hashlib.sha256(archive).hexdigest(), pin(b'archive_sha'))
        self.assertEqual(pin(b'release'), '1.0.3')
        with tarfile.open(fileobj=io.BytesIO(archive), mode='r:gz') as tf:
            data = {m.name: tf.extractfile(m).read() for m in tf if m.isfile()}
            manifest = data['p95-priority-helper/SHA256SUMS']
            self.assertEqual(hashlib.sha256(manifest).hexdigest(), pin(b'manifest_sha'))
            for line in manifest.decode().splitlines():
                digest, name = line.split('  ', 1)
                self.assertEqual(hashlib.sha256(data['p95-priority-helper/'+name]).hexdigest(), digest)
            self.assertEqual(hashlib.sha256(data['p95-priority-helper/p95-priority']).hexdigest(), pin(b'binary_sha'))

    @unittest.skipUnless(sys.platform.startswith('linux'), 'real POSIX file publishing')
    def test_software_copies_and_updates_script_without_managing_helper(self):
        for system, version in [('centos', '7.9'), ('rocky', '9.2'), ('ubuntu', '22.04'), ('debian', '13')]:
            with self.subTest(system=system), tempfile.TemporaryDirectory() as folder:
                root = Path(folder)
                (root/'software-client.json').write_text('{"enabled":true}')
                wanted = dict(os=system, version=version, arch='x86_64' if system in ('centos', 'rocky') else 'amd64')
                # If a downloaded script is ever invoked, fail visibly. Every
                # command entry point is also trapped below, including checks.
                payload = b'#!/bin/sh\necho UNEXPECTED_P95_EXECUTION >&2\nexit 99\n'
                def item():
                    return dict(path='p95-priority-setup.sh', size=len(payload), sha256=hashlib.sha256(payload).hexdigest())
                manifest = dict(schema=1, target=wanted, group=dict(id='fixture', files=[]), copy_files=[item()])
                downloads = []
                class Client:
                    def __init__(self, config): pass
                    def manifest(self, target): return manifest
                    def download(self, file, dest):
                        downloads.append(file['sha256'])
                        Path(dest).write_bytes(payload)
                with patch.object(software, 'HERE', folder), patch.object(software, 'STATE', str(root/'state')), \
                     patch.object(software, 'STAGING', folder), patch.object(software, 'ARCHIVES', str(root/'files')), \
                     patch.object(software, 'target', return_value=wanted), patch.object(software, 'Client', Client), \
                     patch.object(software, 'installed', return_value={'fyhocsys-node': '0.8.6', 'sckocstp-node': '0.8.6'}), \
                     patch.object(software, 'verify'), patch.object(software, 'transaction') as transaction, \
                     patch.object(software, 'run', side_effect=AssertionError('setup invoked a system command')) as command, \
                     contextlib.redirect_stdout(io.StringIO()):
                    software.execute('--list-software')
                    self.assertFalse((root/'files').exists())
                    software.execute('--software')
                    copied = root/'files/p95-priority-setup.sh'
                    self.assertEqual(copied.read_bytes(), payload)
                    self.assertEqual(copied.stat().st_mode & 0o777, 0o600)
                    report_path = root/'state/last-result.json'
                    report = json.loads(report_path.read_text())
                    self.assertEqual(report['status'], 'verified')
                    self.assertNotIn('p95_priority', report)
                    software.execute('--software')
                    software.execute('--check-software')
                    self.assertEqual(len(downloads), 1)
                    # Reports from alpha46 must not make the new checker invoke
                    # an obsolete embedded module or require a P95 service.
                    report['p95_priority'] = [dict(program='fyhocsys', status='verified', version='1.0.1')]
                    report_path.write_text(json.dumps(report))
                    software.execute('--check-software')
                    payload += b'# replacement from controller\n'
                    manifest['copy_files'] = [item()]
                    software.execute('--software')
                    software.execute('--check-software')
                    self.assertEqual(copied.read_bytes(), payload)
                    self.assertEqual(len(downloads), 2)
                    # Preserve the normal integrity/user-edit protection for
                    # the standalone file as well as regular tools downloads.
                    copied.write_bytes(b'local administrator modification\n')
                    with self.assertRaisesRegex(RuntimeError, 'missing or changed'):
                        software.execute('--check-software')
                    with self.assertRaisesRegex(RuntimeError, 'modified or is unmanaged'):
                        software.execute('--software')
                    self.assertEqual(copied.read_bytes(), b'local administrator modification\n')
                    command.assert_not_called()
                    transaction.assert_not_called()


if __name__ == '__main__':
    unittest.main()
