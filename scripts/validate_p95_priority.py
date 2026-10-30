"""P95 setup integration tests; no installation or system service is executed."""
import contextlib
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'internal/center/assets/manual'))
import p95_priority as helper
import software


class PriorityIntegrationTests(unittest.TestCase):
    def test_bundled_script_matches_pin(self):
        helper.verify_setup()

    def test_no_node_changes_nothing(self):
        with patch.object(helper, 'command') as command:
            self.assertEqual(helper.configure({'fyhoc'}), [])
            command.assert_not_called()

    def test_old_systemd_is_explicitly_skipped(self):
        with patch.object(helper.platform, 'machine', return_value='x86_64'), \
                patch.object(helper, 'command', return_value='systemd 218') as command:
            records = helper.configure({'fyhocsys-node'})
            self.assertEqual(records[0]['status'], 'skipped')
            self.assertEqual(command.call_count, 1)
            helper.check(records)
            self.assertEqual(command.call_count, 1)

    def test_centOS7_is_installed_and_checked(self):
        with patch.object(helper.platform, 'machine', return_value='x86_64'), \
                patch.object(helper, 'command', return_value='systemd 219') as command:
            records = helper.configure({'fyhocsys-node'})
            self.assertEqual(records[0]['status'], 'verified')
            command.assert_any_call(['bash', helper.SETUP, '--product', 'fyhocsys'], echo=True)
            command.assert_any_call(['bash', helper.SETUP, '--check', '--product', 'fyhocsys'], echo=True)
            helper.check(records)
            self.assertEqual(command.call_count, 4)

    def test_wrong_architecture_skips_without_commands(self):
        with patch.object(helper.platform, 'machine', return_value='aarch64'), \
                patch.object(helper, 'command') as command:
            self.assertEqual(helper.configure({'fyhocsys-node'})[0]['status'], 'skipped')
            command.assert_not_called()

    def test_configure_and_readonly_check(self):
        with patch.object(helper.platform, 'machine', return_value='x86_64'), \
                patch.object(helper, 'command', return_value='systemd 239') as command:
            records = helper.configure({'fyhocsys-node', 'sckocstp-node'})
            for product in ('fyhocsys','sckocstp'):
                command.assert_any_call(['bash',helper.SETUP,'--product',product],echo=True)
                command.assert_any_call(['bash',helper.SETUP,'--check','--product',product],echo=True)
            command.reset_mock()
            helper.check(records)
            self.assertEqual(command.call_count, 2)
            self.assertTrue(all('--check' in call.args[0] for call in command.call_args_list))

    def test_tampered_script_never_executes(self):
        with tempfile.TemporaryDirectory() as folder:
            script=Path(folder)/'bad.sh'; script.write_text('echo bad\n')
            with patch.object(helper,'SETUP',str(script)), patch.object(helper.platform,'machine',return_value='x86_64'), \
                    patch.object(helper,'command',return_value='systemd 239') as command:
                with self.assertRaisesRegex(RuntimeError,'checksum mismatch'):
                    helper.configure({'fyhocsys-node'})
                self.assertEqual(command.call_count,1)

    def test_install_failure_propagates(self):
        def fail(args,echo=False):
            if args[0]=='systemctl':return 'systemd 239'
            raise RuntimeError('fixture installation failed')
        with patch.object(helper.platform,'machine',return_value='x86_64'), patch.object(helper,'command',side_effect=fail):
            with self.assertRaisesRegex(RuntimeError,'installation failed'):
                helper.configure({'fyhocsys-node'})

    def test_software_failure_retry_and_unchanged_group(self):
        wanted=dict(os='rocky',version='8.10',arch='x86_64')
        manifest=dict(schema=1,target=wanted,group=dict(id='fixture',files=[]))
        records=[dict(program='fyhocsys',status='verified',version=helper.VERSION)]
        class Client:
            def __init__(self,config):pass
            def manifest(self,target):return manifest
        with tempfile.TemporaryDirectory() as folder:
            base=Path(folder)
            (base/'software-client.json').write_text('{"enabled":true}')
            with patch.object(software,'HERE',folder), patch.object(software,'STATE',folder), \
                    patch.object(software,'STAGING',folder), patch.object(software,'target',return_value=wanted), \
                    patch.object(software,'Client',Client), patch.object(software,'installed',return_value={'fyhocsys-node:x86_64':'0.8.6'}), \
                    patch.object(software,'verify'), patch.object(software,'verify_archives'), \
                    patch.object(software,'sync_archives'), patch.object(software,'transaction') as transaction, \
                    patch.object(software.os,'rename',side_effect=os.replace), \
                    patch.object(helper,'configure',side_effect=RuntimeError('fixture failure')) as configure, \
                    patch.object(helper,'check') as check:
                software.execute('--list-software'); configure.assert_not_called()
                with self.assertRaisesRegex(RuntimeError,'fixture failure'):software.execute('--software')
                self.assertEqual(json.loads((base/'last-result.json').read_text())['status'],'failed')
                configure.side_effect=None;configure.return_value=records
                software.execute('--software')
                saved=json.loads((base/'last-result.json').read_text())
                self.assertEqual(saved['status'],'verified');self.assertEqual(saved['p95_priority'],records)
                software.execute('--software')
                self.assertEqual(configure.call_count,3)
                software.execute('--check-software');check.assert_called_once_with(records)
                transaction.assert_not_called()


if __name__=='__main__':
    unittest.main()
