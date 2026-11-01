"""Run the setup E610 adapter against isolated PCI/module/installer fixtures."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
source = (ROOT / 'internal/center/assets/manual/e610-driver.sh').read_text()
IDS = '57AE 57AF 57B0 57B1 57B2'.split()


def run_case(name, hardware=True, native=False, mode='--apply', preflight=0,
             install=0, result=3, marker='', calls=None, corrupt=False):
    with tempfile.TemporaryDirectory() as temp:
        root = Path(temp)
        mock = root / 'bin'
        mock.mkdir()
        pci = root / 'sys/bus/pci/devices/0000:01:00.0'
        pci.mkdir(parents=True)
        (pci / 'vendor').write_text('0x8086\n')
        (pci / 'device').write_text('0x57b1\n' if hardware else '0x1234\n')
        (pci / 'net/eth0').mkdir(parents=True)
        driver = root / 'drivers/ixgbe'
        driver.mkdir(parents=True)
        (pci / 'driver').symlink_to(driver)
        module = root / 'sys/module/ixgbe'
        module.mkdir(parents=True)
        (module / 'srcversion').write_text('GOOD\n' if native else 'OLD\n')
        if native:
            (root / 'installed').touch()
        (root / 'var/log').mkdir(parents=True)
        bundle = root / 'bundle'
        bundle.mkdir()
        installer = bundle / 'install.sh'
        installer.write_text('''#!/bin/bash
set -eu
printf '%s\\n' "$*" >> "$FIXTURE/calls"
echo 'vendor detail only in the private installation log'
test "$1" = --kernel && test "$2" = 6.1-fixture
if [[ ${3:-} == --check ]]; then exit "$PREFLIGHT"; fi
if [[ $INSTALL == 0 ]]; then touch "$FIXTURE/installed"; fi
exit "$INSTALL"
''')
        import hashlib
        digest = hashlib.sha256(installer.read_bytes()).hexdigest()
        (bundle / 'SHA256SUMS').write_text(digest + '  install.sh\n')
        if corrupt:
            installer.write_text(installer.read_text() + '# modified\n')
        aliases = '\n'.join('alias: pci:v00008086d0000' + x for x in IDS)
        commands = {
            'uname': '#!/bin/sh\n[ "$1" != -m ] || { echo x86_64; exit; }; echo 6.1-fixture\n',
            'modinfo': '''#!/bin/bash
[[ -f $FIXTURE/installed ]] || exit 1
case "$*" in
 *alias*) printf '%s\\n' ''' + "'" + aliases + "'" + ''';;
 *srcversion*) echo GOOD;;
 *) exit 1;;
esac
''',
            'modprobe': '#!/bin/sh\necho UNEXPECTED_MODPROBE >&2; exit 1\n',
            'udevadm': '#!/bin/sh\nexit 0\n',
        }
        for name_, text in commands.items():
            path = mock / name_
            path.write_text(text)
            path.chmod(0o755)
        # Explicitly isolate every absolute device/log path. The real adapter
        # has no environment switch that can redirect privileged writes.
        code = source.replace('/sys/', str(root / 'sys') + '/')
        code = code.replace('/var/log/', str(root / 'var/log') + '/')
        code = code.replace('PATH=/usr/sbin:', 'PATH=' + str(mock) + ':/usr/sbin:')
        code = code.replace('[[ $EUID == 0 && ', '[[ ')
        script = root / 'adapter.sh'
        script.write_text(code)
        env = dict(os.environ, FIXTURE=str(root), PREFLIGHT=str(preflight), INSTALL=str(install))
        p = subprocess.run(['bash', str(script), mode, str(bundle)], env=env,
                           capture_output=True, text=True)
        assert p.returncode == result, (name, p.returncode, p.stdout, p.stderr)
        assert marker in p.stdout + p.stderr, (name, marker, p.stdout, p.stderr)
        assert 'vendor detail' not in p.stdout + p.stderr, name
        assert 'UNEXPECTED_MODPROBE' not in p.stderr, name
        got = (root / 'calls').read_text().splitlines() if (root / 'calls').exists() else []
        assert got == (calls or []), (name, got, calls)
        for log in (root / 'var/log').glob('*'):
            assert log.stat().st_mode & 0o077 == 0, (name, 'non-private log')
        print('PASS:', name)


check = '--kernel 6.1-fixture --check'
apply = '--kernel 6.1-fixture'
run_case('no hardware: no installer invoked', hardware=False, result=0, marker='E610_NOT_PRESENT')
run_case('native module kept even with unused corrupt offline bundle', native=True,
         corrupt=True, result=0, marker='E610_DRIVER_VERIFIED')
run_case('check cannot install missing support', mode='--check', marker='E610_DRIVER_PENDING')
run_case('check validates the loaded module', native=True, mode='--check', result=0,
         marker='E610_DRIVER_VERIFIED')
run_case('corrupt vendor payload fails before execution', corrupt=True, result=1,
         marker='checksum mismatch')
run_case('unknown OS/kernel rejected by vendor preflight', preflight=1, result=1,
         marker='preflight failed', calls=[check])
run_case('MOK request stays pending without hidden interactive install', preflight=3,
         marker='E610_MOK_PENDING', calls=[check])
run_case('nested dispatcher receives exact kernel for check and install',
         marker='E610_REBOOT_REQUIRED', calls=[check, apply])
run_case('vendor installation failure is not verified', install=1, result=1,
         marker='installation failed', calls=[check, apply])
run_case('late enrollment requirement remains pending', install=3,
         marker='E610_MOK_PENDING', calls=[check, apply])

