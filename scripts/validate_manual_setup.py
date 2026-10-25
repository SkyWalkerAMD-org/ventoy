"""Offline parser/driver-unpacker regressions; no host configuration changes."""
import importlib.util
import io
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[1]
helper = (ROOT / 'internal/center/assets/safe-server-init.sh').read_text(encoding='utf-8')
probe = helper.split("<<'PY'\n", 1)[1].split('\nPY\n', 1)[0]

with tempfile.TemporaryDirectory() as temp:
    root = Path(temp)
    net = root / 'run/systemd/network'
    net.mkdir(parents=True)
    nic = root / 'sys/class/net/eth1'
    nic.mkdir(parents=True)
    (nic / 'address').write_text('52:54:00:00:00:02\n')
    (nic / 'addr_assign_type').write_text('0\n')
    cases = [
        ('PermanentMACAddress=52:54:00:00:00:01', '0', 1),
        ('PermanentMACAddress=52:54:00:00:00:02', '0', 0),
        ('PermanentMACAddress=52:54:00:00:00:01', '3', 2),
        ('Property=ID_NET_MANAGED_BY=foreign', '0', 2),
        ('Name=other*\nProperty=UNKNOWN=value', '0', 1),
    ]
    for match, assigned, expected in cases:
        (net / '10-test.network').write_text('[Match]\n' + match + '\n')
        (nic / 'addr_assign_type').write_text(assigned + '\n')
        p = subprocess.run([sys.executable, '-c', probe, str(root), 'networkd',
                            'eth1', '52:54:00:00:00:02', 'virtio_net'], capture_output=True)
        assert p.returncode == expected, (match, p.returncode, p.stderr)

spec = importlib.util.spec_from_file_location('extract', ROOT / 'internal/center/assets/manual/e610-extract.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
with tempfile.TemporaryDirectory() as temp:
    root = Path(temp)
    for index, name in enumerate(['../escape', '/absolute', 'payload/link', 'payload/valid']):
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode='w:gz') as archive:
            member = tarfile.TarInfo(name)
            if name.endswith('/link'):
                member.type = tarfile.SYMTYPE
                member.linkname = '/etc/shadow'
            else:
                member.size = 2
            archive.addfile(member, None if member.issym() else io.BytesIO(b'ok'))
        source = root / (str(index) + '.run')
        source.write_bytes(b'#!/bin/sh\n__E610_PAYLOAD_BELOW__\n' + stream.getvalue())
        target = root / str(index)
        target.mkdir()
        if name.endswith('/valid'):
            result = module.extract(str(source), str(target))
            assert (Path(result) / 'valid').read_bytes() == b'ok'
        else:
            try:
                module.extract(str(source), str(target))
            except ValueError:
                pass
            else:
                raise AssertionError('unsafe driver archive accepted: ' + name)
print('PASS: permanent MAC matching, unknown configuration protection, driver archive boundaries')

if sys.platform != 'win32':
    driver = (ROOT / 'internal/center/assets/manual/e610-driver.sh').read_text(encoding='utf-8')
    verify = 'verify() {' + driver.split('verify() {', 1)[1].split('if [[ $mode == --check ]]', 1)[0]
    code = 'set -e; kver=test; native_support() { return 1; };\n' + verify
    code += '\nrc=0; verify || rc=$?; test "$rc" = 3\n'
    subprocess.run(['bash', '-c', code], check=True)
    print('PASS: E610 waiting for installation/MOK remains pending, not verified or failed')

    # Exercise real login verification across an asynchronous sshd reload.
    # A correct-looking username with a failed SSH exit status is not success.
    source = (ROOT / 'internal/center/assets/manual/manual-ssh.sh').read_text(encoding='utf-8')
    enable = 'enable_ssh() {' + source.split('enable_ssh() {', 1)[1].split('\n    if ! sshd -t', 1)[0]
    for family, enabled, active, expected in [('apt', 0, 0, ''), ('apt', 1, 0, 'enable ssh.service'),
                                              ('apt', 0, 1, 'enable ssh.service'), ('rpm', 0, 0, 'enable sshd.service')]:
        mocks = '''set -e
systemctl() {
 case "$1" in
  is-enabled) return "$enabled";;
  is-active) return "$active";;
  enable) echo "$*";;
  *) exit 99;;
 esac
}
'''
        code = mocks + '\nfamily=' + family + '\nenabled=' + str(enabled) + '\nactive=' + str(active)
        code += '\nservice=' + ('ssh' if family == 'apt' else 'sshd') + '\n' + enable + '\nenable_ssh\n'
        result = subprocess.run(['bash', '-c', code], capture_output=True, text=True)
        assert result.returncode == 0 and result.stdout.strip() == expected, result
    print('PASS: existing socket activation retained; inactive and legacy service paths enabled')
    start = source.index('    for user in root admin; do\n        # Some units reload')
    end = source.index('\nelse\n    echo "SSH_CONFIG_VERIFIED', start)
    login_loop = source[start:end]
    scenarios = [
        ('--apply', 'transition', 0, 4),
        ('--apply', 'denied', 1, 3),
        ('--check', 'denied', 1, 1),
        ('--apply', 'wrong', 1, 3),
        ('--apply', 'nonzero', 1, 3),
    ]
    for mode, scenario, expected, calls in scenarios:
        with tempfile.TemporaryDirectory(prefix='manual-ssh-regression-') as temp:
            mocks = r'''set -eu
die() { echo "$*" >&2; exit 1; }
port=22
setsid() {
    n=$(cat "$scratch/calls" 2>/dev/null || echo 0)
    n=$((n+1)); printf '%s\n' "$n" > "$scratch/calls"
    case "$scenario" in
        transition)
            if [ "$n" -le 2 ]; then return 255; fi
            printf '%s\n' "$user";;
        denied) return 255;;
        wrong) echo other-user;;
        nonzero) printf '%s\n' "$user"; return 255;;
    esac
}
sleep() { :; }
'''
            import shlex
            code = mocks + '\nscratch=' + shlex.quote(temp)
            code += '\nmode=' + mode + '\nscenario=' + scenario + '\n' + login_loop
            result = subprocess.run(['bash', '-c', code], capture_output=True, text=True)
            actual_calls = int((Path(temp) / 'calls').read_text())
            assert (result.returncode, actual_calls) == (expected, calls), (scenario, result.stderr)
    print('PASS: SSH reload readiness, permanent denial, check-only mode and authenticated identity')

    # Run the actual completion path with only the software transaction mocked.
    # A connected primary NIC must still permit software installation when a
    # secondary NIC or driver is pending. Do not replace pending with success.
    source = (ROOT / 'internal/center/assets/manual/install.sh').read_text(encoding='utf-8')
    completion = source[source.index('[[ $(systemctl get-default) == multi-user.target ]]'):]
    scenarios = [
        # pending, mode, offline, request, software_rc, expected_rc, calls, request_after
        (1, '--resume', 0, '1', 0, 3, 1, '0'),
        (0, '--resume', 0, '1', 0, 0, 1, '0'),
        (1, '--resume', 0, '1', 17, 17, 1, '1'),
        (0, '--resume', 0, '1', 17, 17, 1, '1'),
        (1, '--check', 0, '1', 0, 3, 0, '1'),
        (0, '--check', 0, '1', 0, 0, 0, '1'),
        (1, '--resume', 1, '1', 0, 3, 0, '1'),
        (1, '--resume', 0, '0', 0, 3, 0, '0'),
        (0, '--resume', 0, '0', 0, 0, 0, '0'),
        (1, '--resume', 0, None, 0, 3, 0, None),
    ]
    for pending, mode, offline, request, software_rc, expected_rc, calls, request_after in scenarios:
        with tempfile.TemporaryDirectory(prefix='manual-software-regression-') as temp:
            state = Path(temp)
            request_file = state / 'software-request'
            if request is not None:
                request_file.write_text(request + '\n')
            mocks = r'''set -Eeuo pipefail
die() { echo "$*" >&2; exit 1; }
systemctl() { test "$*" = 'get-default'; echo multi-user.target; }
software_action() {
    test "$1" = --software
    echo invoked >> "$state/calls"
    return "$software_rc"
}
'''
            code = mocks + '\nstate=' + shlex.quote(temp)
            code += '\npending=%d\nmode=%s\noffline=%d\nsoftware_rc=%d\n' % (pending, mode, offline, software_rc)
            code += completion
            result = subprocess.run(['bash', '-c', code], capture_output=True, text=True)
            actual_calls = len((state / 'calls').read_text().splitlines()) if (state / 'calls').exists() else 0
            actual_request = request_file.read_text().strip() if request_file.exists() else None
            assert (result.returncode, actual_calls, actual_request) == (expected_rc, calls, request_after), result
            if software_rc and calls:
                assert not (state / 'status').exists(), 'failed software transaction was reported as completed'
                assert not any(line.startswith(('PENDING:', 'VERIFIED:')) for line in result.stdout.splitlines())
            else:
                status = (state / 'status').read_text()
                assert status.startswith('pending ' if pending else 'verified '), status
            # Once fulfilled, a normal boot/resume must not fetch software again,
            # even when an unrelated check still remains pending.
            if calls and software_rc == 0:
                repeat = subprocess.run(['bash', '-c', code], capture_output=True, text=True)
                assert repeat.returncode == expected_rc, repeat
                assert len((state / 'calls').read_text().splitlines()) == 1
            # Failure must leave the original explicit request available to retry.
            if software_rc and calls:
                retry = subprocess.run(['bash', '-c', code.replace('software_rc=17', 'software_rc=0')], capture_output=True, text=True)
                assert retry.returncode == (3 if pending else 0), retry
                assert request_file.read_text().strip() == '0'
    print('PASS: pending system checks do not skip software; failure/retry, no repeat upgrade, offline and check-only modes')
