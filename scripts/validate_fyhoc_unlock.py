"""Real PTY authentication tests, with no host service or credential changes."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / 'internal/center/assets/manual/fyhoc_unlock.py'


@unittest.skipUnless(sys.platform.startswith('linux'), 'Linux controlling-terminal tests')
class UnlockTests(unittest.TestCase):
    def test_terminal_password_and_boot_verification(self):
        spec = importlib.util.spec_from_file_location('unlock', SOURCE)
        mod = importlib.util.module_from_spec(spec); spec.loader.exec_module(mod)
        with tempfile.TemporaryDirectory() as folder:
            state = Path(folder)/'unlocked'
            program = Path(folder)/'fyhoc'
            # Like the real utility, only /dev/tty is accepted; already unlocked
            # succeeds with no terminal. No environment/stdin password backdoor.
            template = '''#!{python}
import os,sys,termios
state={state!r}
if os.path.exists(state): sys.exit(0)
try: fd=os.open('/dev/tty',os.O_RDWR)
except OSError: sys.exit(1)
t=termios.tcgetattr(fd); t[3] &= ~(termios.ECHO|termios.ECHONL)
termios.tcsetattr(fd,termios.TCSAFLUSH,t)
os.write(fd,b'fyhoc password: ')
key=os.read(fd,128).strip()
if key != b'fixture-secret': sys.exit(1)
open(state,'w').write('this boot')
'''
            program.write_text(template.format(python=sys.executable,state=str(state))); program.chmod(0o700)
            self.assertFalse(mod.unlocked(str(program)))
            mod.authenticate(str(program), b'fixture-secret')
            self.assertTrue(mod.unlocked(str(program)))
            mod.authenticate(str(program), b'fixture-secret')
            state.unlink()  # A new boot no longer has the earlier /run session.
            self.assertFalse(mod.unlocked(str(program)))
            with self.assertRaisesRegex(RuntimeError, 'unlock failed'):
                mod.authenticate(str(program), b'wrong')
            self.assertFalse(state.exists())
            mod.authenticate(str(program), b'fixture-secret')
            state.unlink()
            # A changed program that leaves echo enabled never receives a key.
            program.write_text(template.replace('t[3] &= ~(termios.ECHO|termios.ECHONL)', 't[3] |= termios.ECHO').format(python=sys.executable,state=str(state)))
            with self.assertRaisesRegex(RuntimeError, 'echo enabled'):
                mod.authenticate(str(program), b'fixture-secret')
            self.assertFalse(state.exists())
            program.write_text('#!'+sys.executable+'\nimport time;time.sleep(5)\n')
            with patch.object(mod, 'TIMEOUT', .2):
                with self.assertRaisesRegex(RuntimeError, 'timed out'):
                    mod.authenticate(str(program), b'fixture-secret')

    def test_password_validation(self):
        spec = importlib.util.spec_from_file_location('unlock', SOURCE)
        mod = importlib.util.module_from_spec(spec); spec.loader.exec_module(mod)
        for value in ('', 'x\n', 'x y', 'x'*128, None, '非ASCII'):
            with self.assertRaises(RuntimeError): mod.password_bytes(value)
        self.assertEqual(mod.password_bytes('fixture-key_123'), b'fixture-key_123')


if __name__ == '__main__':
    unittest.main()
