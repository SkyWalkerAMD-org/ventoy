"""Run the shared setup hostname transaction against an isolated filesystem."""
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
source = (ROOT / 'internal/center/assets/manual/install.sh').read_text(encoding='utf-8')
start = source.index('    # The current persistent hostname')
end = source.index('    if [[ ! -e "$state/configured"', start)
transaction = source[start:end]
check = next(line for line in source.splitlines()
             if line.startswith('[[ -s /etc/hostname &&'))
request = source[source.index('    # This is a one-time request'):source.index('    printf \'%s\\n\' "$offline"')]
chooser = source[source.index('choose_hostname() {'):source.index('if [[ $mode == --apply ]]; then')]

if sys.platform == 'win32':
    print('SKIP: hostname shell regression runs on Linux CI')
    sys.exit(0)

for mode in ('--software', '--list-software', '--check-software', '--resume'):
    result = subprocess.run(['bash', str(ROOT/'internal/center/assets/manual/install.sh'), mode, '--hostname', 'node-intended'], capture_output=True, text=True)
    assert result.returncode == 2 and '--hostname requires full setup' in result.stderr, result

with tempfile.TemporaryDirectory(prefix='ventoy-hostname-') as temp:
    root = Path(temp)
    state = root / 'state'
    etc = root / 'etc'
    here = root / 'package'
    for path in (state, here, etc / 'cloud/cloud.cfg.d'):
        path.mkdir(parents=True, exist_ok=True)
    (etc / 'machine-id').write_text('abcdef0123456789abcdef0123456789\n')
    # Only hostnamectl's side effect is simulated. The production transaction,
    # request creation and check run unchanged, including their error handling.
    (here / 'safe-server-init.sh').write_text('''#!/bin/bash
set -eu
printf '%s\\n' "$*" >> "$scratch/calls"
[[ $1 == hostname && $3 == --update-hosts ]] || exit 98
[[ $2 != invalid_name ]] || exit 2
[[ ${fixture_skip_hostname:-0} != 1 ]] || exit 0
name=${2,,}
printf '%s\\n' "$name" > "$scratch/etc/hostname"
printf '%s\\n' "$name" > "$scratch/runtime"
''')
    prefix = '''set -Eeuo pipefail
die() { echo "$*" >&2; exit 1; }
hostname() { cat "$scratch/runtime"; }
'''
    prefix += '\nexport scratch=' + shlex.quote(str(root))
    prefix += '\nstate=' + shlex.quote(str(state))
    prefix += '\nhere=' + shlex.quote(str(here)) + '\n'

    def run(code, expected=0):
        code = code.replace('/etc/', str(etc) + '/')
        result = subprocess.run(['bash', '-c', prefix + code], text=True, capture_output=True)
        assert result.returncode == expected, (result.returncode, result.stdout, result.stderr)
        return result

    def rename(name):
        (etc / 'hostname').write_text(name + '\n')
        (root / 'runtime').write_text(name + '\n')

    def verify(name):
        assert (etc / 'hostname').read_text().strip() == name
        assert (root / 'runtime').read_text().strip() == name
        run(check)

    def calls():
        path = root / 'calls'
        return path.read_text().splitlines() if path.exists() else []

    # Upgrade from a sticky old --hostname request, stale snapshot and a prior
    # failed/pending setup. A boot retry must respect a later administrator edit.
    rename('operator-new')
    (state / 'hostname').write_text('server-issued-old\n')
    (state / 'hostname-request').write_text('explicit-old\n')
    (state / 'status').write_text('failed exit=1\n')
    run(check)  # read-only checks do not require the stale cached name
    assert (state / 'hostname-request').exists()
    run(transaction)
    verify('operator-new')
    assert not calls()
    assert (state / 'hostname').read_text() == 'operator-new\n'
    assert not (state / 'hostname-request').exists()
    assert (etc / 'cloud/cloud.cfg.d/99-ventoy-manual-hostname.cfg').read_text() == 'preserve_hostname: true\n'

    # A fresh explicit request works once; the next operator rename survives
    # both the check and repeated service/interactive resumes.
    run('name=Explicit-New\n' + request + '\n' + transaction)
    verify('explicit-new')
    assert calls() == ['hostname explicit-new --update-hosts']
    assert not (state / 'hostname-pending').exists()
    rename('operator-later')
    for _ in range(2):
        run(transaction)
        verify('operator-later')
    assert len(calls()) == 1

    # No implicit case conversion or unnecessary hosts edits on existing names.
    rename('Existing-Name')
    run(transaction)
    verify('Existing-Name')
    assert len(calls()) == 1

    # A hostname command alone is transient: the persistent value wins on boot.
    (root / 'runtime').write_text('transient-only\n')
    run(check, 1)
    run(transaction)
    verify('existing-name')

    # First installation still supplies a useful name when there is none.
    for name in ('', 'localhost', 'localhost.localdomain'):
        rename(name)
        run(transaction)
        verify('linux-abcdef01')

    # Failure keeps the new explicit request for recovery, but never records a
    # success or silently replaces the requested name with an old snapshot.
    rename('unchanged-on-failure')
    observed = (state / 'hostname').read_text()
    run('name=invalid_name\n' + request + '\n' + transaction, 2)
    verify('unchanged-on-failure')
    assert (state / 'hostname-pending').read_text() == 'invalid_name\n'
    assert (state / 'hostname').read_text() == observed
    run('name=\n' + request + '\n' + transaction)
    verify('unchanged-on-failure')
    assert not (state / 'hostname-pending').exists()
    run('name=recovered\n' + request + '\n' + transaction)
    verify('recovered')
    assert not (state / 'hostname-pending').exists()

    # The software stage includes enrollment. It must observe the requested
    # hostname in both places, after the pending request was consumed. A boot
    # retry follows the same order, and even a lying successful helper cannot
    # let enrollment use the old/default hostname.
    completion = source[source.index('[[ $(systemctl get-default)'):]
    assert source.index('    choose_hostname\n') < source.index('    # The current persistent hostname')
    assert source.index('    # The current persistent hostname') < source.index(check) < source.index(completion)
    enrollment = '''
pending=0; mode=--resume; offline=0
systemctl() { echo multi-user.target; }
software_action() {
    [[ $(hostname) == "$fixture_expected" && $(cat /etc/hostname) == "$fixture_expected" ]]
    [[ ! -e "$state/hostname-pending" ]]
    printf '%s\\n' "$(hostname)" >> "$scratch/bindings"
}
'''
    rename('ubuntu')
    (state / 'software-request').write_text('1\n')
    run('fixture_expected=node-final\nname=node-final\n' + enrollment + request + transaction + check + '\n' + completion)
    assert (root / 'bindings').read_text().splitlines() == ['node-final']
    rename('ubuntu')
    (state / 'hostname-pending').write_text('node-after-reboot\n')
    (state / 'software-request').write_text('1\n')
    run('fixture_expected=node-after-reboot\n' + enrollment + transaction + check + '\n' + completion)
    assert (root / 'bindings').read_text().splitlines() == ['node-final', 'node-after-reboot']
    for name, skip, code in [('invalid_name', 0, 2), ('never-applied', 1, 1)]:
        rename('ubuntu')
        (state / 'hostname-pending').write_text(name + '\n')
        (state / 'software-request').write_text('1\n')
        run('export fixture_skip_hostname=%s\nfixture_expected=%s\n' % (skip, name) + enrollment + transaction + check + '\n' + completion, code)
        assert (root / 'bindings').read_text().splitlines() == ['node-final', 'node-after-reboot']
        assert (state / 'hostname-pending').exists() and (state / 'software-request').read_text() == '1\n'

print('PASS: hostname edits survive stale setup state, explicit requests apply once, boot retries and read-only checks preserve administrator choices')
print('PASS: software/enrollment follows the final hostname on initial setup and boot retry; hostname failure or false success cannot bind the old name')

# A real pseudo-terminal exercises the default interactive experience, while
# pipe-based automation, explicit arguments and boot recovery stay unattended.
import os
import pty
import select
helper = ROOT / 'internal/center/assets/safe-server-init.sh'
choose_code = 'set -Eeuo pipefail\nsource ' + shlex.quote(str(helper)) + '\n' + chooser
for name, expected in [('', ''), ('Explicit-Node', 'explicit-node')]:
    code = choose_code + '\nname=' + shlex.quote(name) + '\nchoose_hostname\nprintf "RESULT=%s\\n" "$name"\n'
    result = subprocess.run(['bash', '-c', code], input='', text=True, capture_output=True)
    assert result.returncode == 0 and result.stdout == 'RESULT=' + expected + '\n', result
    assert 'New hostname' not in result.stderr

for answer, expected, exit_code in [(b'New-Server\n', 'new-server', 0),
                                    (b'\n', '', 0),
                                    (b'invalid_name\nvalid-name\n', 'valid-name', 0),
                                    (b'invalid_name\n127.0.0.1\nlocalhost\n', None, 2),
                                    (b'\x04', None, 2)]:
    master, slave = pty.openpty()
    process = subprocess.Popen(['bash', '-c', choose_code + '\nname=\nchoose_hostname\nprintf "RESULT=%s\\n" "$name"\n'],
                               stdin=slave, stdout=slave, stderr=slave, close_fds=True)
    os.close(slave)
    output = b''
    sent = False
    deadline = time.monotonic() + 8
    try:
        while time.monotonic() < deadline:
            if select.select([master], [], [], .1)[0]:
                try:
                    data = os.read(master, 4096)
                except OSError:
                    break  # Linux PTYs return EIO after the child closes them.
                if not data:
                    break
                output += data
                if not sent and b'New hostname (Enter to keep current): ' in output:
                    os.write(master, answer)
                    sent = True
        assert sent, output
        assert process.wait(timeout=2) == exit_code, output
        if expected is not None:
            assert ('RESULT=' + expected + '\r\n').encode() in output, output
        else:
            assert b'RESULT=' not in output, output
    finally:
        os.close(master)
        if process.poll() is None:
            process.kill()
        process.wait()
print('PASS: interactive hostname input, Enter-to-keep, invalid-name retry/cancel and non-interactive preservation')
