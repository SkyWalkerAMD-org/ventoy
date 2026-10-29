"""Node enrollment protocol boundaries, subprocess secrecy and software retries.

All commands run against fixture files; no production center or service changes.
"""
import contextlib
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import ssl
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT/'internal/center/assets/manual'
sys.path.insert(0, str(ASSETS))
import node_join as join
import software

KEY = 'ABCD' * 8


def node(program='fyhocsys'):
    return dict(program=program, url='https://center.invalid', pin='a'*64, key=KEY)


class JoinTests(unittest.TestCase):
    def test_hostname_must_finish_before_any_binding(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root/'hostname').write_text('node-final\n')
            with patch.object(join, 'CONFIG_ROOT', folder), patch.object(join, 'MANUAL_STATE', folder), \
                 patch.object(join.socket, 'gethostname', return_value='node-final') as hostname, \
                 patch.object(join, 'command') as command:
                self.assertEqual(join.ready_hostname(), 'node-final')
                (root/'hostname-pending').write_text('node-new\n')
                with self.assertRaisesRegex(RuntimeError, 'requested hostname'):
                    join.configure([node()], {'fyhocsys-node'})
                (root/'hostname-pending').unlink()
                hostname.return_value = 'transient-host'
                with self.assertRaisesRegex(RuntimeError, 'matching runtime'):
                    join.configure([node()], {'fyhocsys-node'})
                hostname.return_value = 'ubuntu'
                (root/'hostname').write_text('ubuntu\n')
                with self.assertRaisesRegex(RuntimeError, 'unique hostname'):
                    join.configure([node()], {'fyhocsys-node'})
                command.assert_not_called()
                hostname.return_value = 'node-final'
                (root/'hostname').write_text('node-final\n')
                def change_during_version(args, description):
                    self.assertEqual(args[1], 'version')
                    hostname.return_value = 'changed-again'
                    (root/'hostname').write_text('changed-again\n')
                    return 'fyhocsys 0.8.6'
                command.side_effect = change_during_version
                with patch.object(join, 'trusted_binary', return_value='/fixture/fyhocsys-node'):
                    with self.assertRaisesRegex(RuntimeError, 'Hostname changed'):
                        join.configure([node()], {'fyhocsys-node'})
                self.assertEqual(command.call_count, 1)

    def test_local_check_requires_binding_worker_and_boot_service(self):
        record = {k: v for k, v in node().items() if k != 'key'}
        with patch.object(join, 'trusted_binary', return_value='/fixture/fyhocsys-node'), \
             patch.object(join, 'ready_hostname', return_value='fixture-node'), \
             patch.object(join, 'connection') as connection:
            for broken in ('worker', 'disabled', 'inactive'):
                def command(args, description):
                    if args[0] != 'systemctl':
                        if broken == 'worker': raise RuntimeError('worker check failed')
                        return ''
                    if args[1] == 'is-enabled': return 'disabled' if broken == 'disabled' else 'enabled'
                    return 'inactive' if broken == 'inactive' else 'active'
                with patch.object(join, 'command', side_effect=command):
                    with self.assertRaises(RuntimeError): join.check([record])
            connection.side_effect = RuntimeError('wrong center')
            with patch.object(join, 'command') as command:
                with self.assertRaisesRegex(RuntimeError, 'wrong center'): join.check([record])
                command.assert_not_called()

    def test_private_response_and_invalid_inputs(self):
        class Client:
            def get(self, endpoint, params):
                self.endpoint = endpoint
                return io.BytesIO(json.dumps(response).encode())
        api = Client()
        response = dict(enabled=False)
        self.assertEqual(join.fetch(api), [])
        response = dict(enabled=True, schema=1, nodes=[node(), node('sckocstp')])
        self.assertEqual(len(join.fetch(api)), 2)
        self.assertEqual(api.endpoint, 'node-join')
        for field, value in [('program', 'sh'), ('url', 'http://center.invalid'),
                             ('url', 'https://user:pass@center.invalid'), ('url', 'https://center.invalid/path'),
                             ('url', 'https://center.invalid?'), ('url', 'https://center.invalid#'),
                             ('pin', 'x'*64), ('key', '0'*32), ('key', KEY+'\n'), ('key', None)]:
            with self.subTest(field=field, value=value):
                response = dict(enabled=True, schema=1, nodes=[dict(node(), **{field: value})])
                with self.assertRaisesRegex(RuntimeError, 'Invalid private'):
                    join.fetch(api)
        for nodes in ([], [node(), node()], 'bad'):
            response = dict(enabled=True, schema=1, nodes=nodes)
            with self.assertRaises(RuntimeError): join.fetch(api)

    def test_real_subprocess_key_file_cleanup_failures_and_retry(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            state = root/'state.json'
            calls = root/'calls.jsonl'
            binary = root/'fyhocsys-node'
            binary.write_text('''#!%s
import json,os,stat,sys,time
from pathlib import Path
root=Path(__file__).parent
state=json.loads((root/'state.json').read_text())
action=sys.argv[1]
if action=='version': print(Path(__file__).name[:-5]+' '+state.get('version','0.8.6')); sys.exit(0)
if action=='check': sys.exit(0)
assert action=='join' and '--key' not in sys.argv
path=Path(sys.argv[sys.argv.index('--key-file')+1])
key=path.read_text().strip()
assert path.stat().st_mode & 0o777 == 0o600
assert path.parent.stat().st_mode & 0o777 == 0o700
assert key not in str(sys.argv) and key not in str(dict(os.environ))
with open(root/'calls.jsonl','a') as f: f.write(json.dumps(sys.argv)+'\\n')
if state.get('fail'):
    print(key); print('untrusted server output '+key); sys.exit(1)
if state.get('timeout'): time.sleep(10)
(root/'binding').write_text('existing binding preserved')
''' % sys.executable)
            binary.chmod(0o700)
            state.write_text('{}')
            real_command = join.command
            services = []
            def command(args, description):
                if args[0] != 'systemctl': return real_command(args, description)
                services.append(args)
                return {'is-enabled': 'enabled', 'is-active': 'active'}.get(args[1], '')
            with patch.object(join, 'KEY_ROOT', folder), patch.object(join, 'trusted_binary', return_value=str(binary)), \
                 patch.object(join, 'ready_hostname', return_value='fixture-node'), \
                 patch.object(join, 'connection'), patch.object(join, 'command', side_effect=command):
                records = join.configure([node()], {'fyhocsys-node'})
                self.assertEqual(records, [{k: v for k, v in node().items() if k != 'key'}])
                join.configure([node()], {'fyhocsys-node'})
                self.assertTrue((root/'binding').exists())
                self.assertFalse(list(root.glob('ventoy-node-join-*')))
                self.assertNotIn(KEY, calls.read_text())
                self.assertFalse(any(c[1] == 'restart' for c in services))
                for condition in ({'fail': True}, {'timeout': True}):
                    state.write_text(json.dumps(condition))
                    output = io.StringIO()
                    with contextlib.redirect_stdout(output), patch.object(join, 'TIMEOUT', .2):
                        with self.assertRaises(RuntimeError) as caught:
                            join.configure([node()], {'fyhocsys-node'})
                    self.assertNotIn(KEY, str(caught.exception) + output.getvalue())
                    self.assertNotIn('untrusted server output', str(caught.exception) + output.getvalue())
                    self.assertFalse(list(root.glob('ventoy-node-join-*')))
                state.write_text('{"version":"0.8.5"}')
                count = len(calls.read_text().splitlines())
                with self.assertRaisesRegex(RuntimeError, '0.8.6'):
                    join.configure([node()], {'fyhocsys-node'})
                self.assertEqual(len(calls.read_text().splitlines()), count)
                state.write_text('{}')
                join.configure([node()], {'fyhocsys-node'})
                with self.assertRaisesRegex(RuntimeError, 'publish and install'):
                    join.configure([node()], set())
                second = root/'sckocstp-node'
                second.write_bytes(binary.read_bytes()); second.chmod(0o700)
                with patch.object(join, 'trusted_binary', side_effect=lambda p: str(root/(p+'-node'))):
                    result = join.configure([node(), node('sckocstp')], {'fyhocsys-node', 'sckocstp-node'})
                    self.assertEqual([r['program'] for r in result], ['fyhocsys', 'sckocstp'])

    @unittest.skipUnless(os.geteuid() == 0, 'root ownership check; run in disposable container')
    def test_real_connection_certificate_hostname_and_permissions(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                            '-subj', '/CN=fixture.invalid', '-keyout', str(root/'key'), '-out', str(root/'cert')],
                           check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            cert = (root/'cert').read_text()
            wanted = dict(node(), pin=hashlib.sha256(ssl.PEM_cert_to_DER_cert(cert)).hexdigest())
            path = root/'fyhocsys/node/connection.json'
            path.parent.mkdir(parents=True)
            config = dict(url=wanted['url'], node=join.socket.gethostname(), ca_pem=cert)
            path.write_text(json.dumps(config)); path.chmod(0o600)
            with patch.object(join, 'CONFIG_ROOT', folder):
                join.connection(wanted)
                for field, value in [('url', 'https://other.invalid'), ('pin', 'b'*64)]:
                    with self.assertRaises(RuntimeError): join.connection(dict(wanted, **{field: value}))
                with patch.object(join.socket, 'gethostname', return_value='different-host'):
                    with self.assertRaises(RuntimeError): join.connection(wanted)
                path.chmod(0o644)
                with self.assertRaises(RuntimeError): join.connection(wanted)
                path.chmod(0o600)
                kept = root/'kept.json'; path.rename(kept); path.symlink_to(kept)
                with self.assertRaises(RuntimeError): join.connection(wanted)

    def test_software_join_without_new_packages_failure_retry_and_offline_check(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root/'software-client.json').write_text('{"enabled":true,"node_join":true}')
            target = dict(os='ubuntu', version='22.04', arch='amd64')
            events = []
            class Client:
                def __init__(self, config): pass
                def manifest(self, wanted): return dict(schema=1, target=wanted, group=None)
            records = [{k: v for k, v in node().items() if k != 'key'}]
            with patch.object(software, 'HERE', folder), patch.object(software, 'STAGING', folder), \
                 patch.object(software, 'STATE', str(root/'state')), patch.object(software, 'Client', Client), \
                 patch.object(software, 'target', return_value=target), \
                 patch.object(software, 'installed', return_value={'fyhocsys-node:amd64': '0.8.6'}), \
                 patch.object(join, 'fetch', return_value=[node()]) as fetch, \
                 patch.object(join, 'configure', side_effect=RuntimeError('fixture join failed')) as configure, \
                 patch.object(join, 'check') as check:
                software.execute('--list-software')
                fetch.assert_not_called()
                with self.assertRaisesRegex(RuntimeError, 'fixture join failed'):
                    software.execute('--software')
                report = root/'state/last-result.json'
                self.assertEqual(json.loads(report.read_text())['status'], 'failed')
                self.assertNotIn(KEY, report.read_text())
                configure.side_effect = None; configure.return_value = records
                software.execute('--software')
                configure.assert_called_with([node()], {'fyhocsys-node'})
                self.assertEqual(json.loads(report.read_text())['node_join'], records)
                fetch.reset_mock()
                software.execute('--check-software')
                fetch.assert_not_called(); check.assert_called_with(records)
                check.side_effect = RuntimeError('service stopped')
                with self.assertRaisesRegex(RuntimeError, 'service stopped'):
                    software.execute('--check-software')


if __name__ == '__main__':
    unittest.main()
