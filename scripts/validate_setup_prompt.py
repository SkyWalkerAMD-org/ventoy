"""Exercise the actual self-extracting launcher on a PTY with an inert payload."""
import errno
import hashlib
import io
import os
from pathlib import Path
import select
import subprocess
import sys
import tarfile
import tempfile
import time

if not sys.platform.startswith('linux'):
    print('SKIP: Linux PTY launcher tests')
    sys.exit(0)
import pty

ROOT = Path(__file__).resolve().parents[1]
template = (ROOT / 'internal/center/assets/manual-launcher.sh').read_text()
# CI is unprivileged. Replace only the top-level root guard; the test payload
# writes a fixture file, never system configuration or the actual hostname.
template = template.replace("[[ $EUID == 0 ]] || { echo 'Run with root or sudo.' >&2; exit 1; }", ':')

with tempfile.TemporaryDirectory(prefix='vc-prompt-') as folder:
    root = Path(folder)
    (root / 'bin').mkdir()
    (root / 'home').mkdir()
    for name, content in {
        'hostname': '#!/bin/sh\ncat "$FIXTURE_HOSTNAME"\n',
        'id': '#!/bin/sh\n[ "$*" = "-u fixture-user" ] && echo 1234\n',
        'runuser': '#!/bin/sh\n[ "$*" = "-u fixture-user -- /bin/bash -i" ] || exit 99\necho CALLER_ACCOUNT_REFRESH\n',
        'ps': '#!/bin/sh\nif [ "${FIXTURE_SUDO:-}" = 1 ]; then\n case "$*" in "-o comm="*) echo sudo; exit;; "-o ruid="*) echo 1234; exit;; esac\nfi\nexec /bin/ps "$@"\n',
    }.items():
        p = root / 'bin' / name
        p.write_text(content)
        p.chmod(0o755)
    # A real interactive Bash reads this file. Its marker proves the launcher
    # actually entered a fresh shell; exiting prevents a CI prompt from hanging.
    (root / 'home/.bashrc').write_text('printf "FRESH_BASH cwd=%s\\n" "$PWD"\nexit 0\n')
    payload = b'''#!/bin/bash
set -eu
if [ "${FIXTURE_RENAME:-1}" = 1 ]; then printf 'new-host\n' > "$FIXTURE_HOSTNAME"; fi
echo SETUP_PAYLOAD_DONE
exit "${FIXTURE_RC:-0}"
'''
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w:gz') as archive:
        for name, body in [('install.sh', payload), ('root-password.sh', b'#!/bin/bash\necho PASSWORD_DIALOG\n')]:
            item = tarfile.TarInfo('ventoy-manual-setup/' + name)
            item.mode = 0o755
            item.size = len(body)
            archive.addfile(item, io.BytesIO(body))
    data = stream.getvalue()
    launcher = root / 'setup.run'
    launcher.write_bytes(template.replace('@ARCHIVE_SHA256@', hashlib.sha256(data).hexdigest()).encode() + data)
    launcher.chmod(0o755)

    def run(args=(), rename=True, rc=0, terminal=True, sudo=False, stale_sudo=False):
        (root / 'hostname').write_text('old-host\n')
        env = dict(os.environ, HOME=str(root / 'home'), PATH=str(root / 'bin') + ':' + os.environ['PATH'],
                   FIXTURE_HOSTNAME=str(root / 'hostname'), FIXTURE_RENAME=str(int(rename)), FIXTURE_RC=str(rc), FIXTURE_SUDO=str(int(sudo)))
        for key in ('SUDO_USER', 'SUDO_UID', 'BASH_ENV', 'ENV'):
            env.pop(key, None)
        if sudo or stale_sudo:
            env.update(SUDO_USER='fixture-user', SUDO_UID='1234')
        command = ['/bin/bash', str(launcher), *args]
        if not terminal:
            p = subprocess.run(command, cwd=root, env=env, capture_output=True, text=True, timeout=10)
            return p.returncode, p.stdout + p.stderr
        pid, fd = pty.fork()
        if pid == 0:
            os.chdir(root)
            os.execve(command[0], command, env)
        output = b''
        status = None
        try:
            end = time.monotonic() + 10
            while time.monotonic() < end:
                ready, _, _ = select.select([fd], [], [], .1)
                if ready:
                    try:
                        output += os.read(fd, 65536)
                    except OSError as e:
                        if e.errno != errno.EIO:
                            raise
                done, value = os.waitpid(pid, os.WNOHANG)
                if done:
                    status = value
                    break
            assert status is not None, output
        finally:
            if status is None:
                os.kill(pid, 9)
                os.waitpid(pid, 0)
            os.close(fd)
        return os.waitstatus_to_exitcode(status), output.decode(errors='replace')

    code, out = run()
    assert code == 0 and 'PASSWORD_DIALOG' in out and 'HOSTNAME_VERIFIED: new-host' in out and 'FRESH_BASH cwd=' + str(root) in out, out
    for options in ({'rename': False}, {'terminal': False}, {'args': ['--check']},
                    {'args': ['--resume']}, {'args': ['--software']}, {'rc': 17}, {'rc': 3}):
        code, out = run(**options)
        assert code == options.get('rc', 0) and 'FRESH_BASH' not in out and 'HOSTNAME_VERIFIED:' not in out, (options, code, out)
        wanted = options.get('terminal', True) and options.get('rc', 0) in (0, 3) and not options.get('args')
        assert ('PASSWORD_DIALOG' in out) == wanted, (options, out)
    code, out = run(sudo=True)
    assert code == 0 and 'CALLER_ACCOUNT_REFRESH' in out and 'FRESH_BASH' not in out, out
    code, out = run(stale_sudo=True)
    assert code == 0 and 'FRESH_BASH' in out and 'CALLER_ACCOUNT_REFRESH' not in out, out
print('PASS: fresh interactive prompt, same directory, sudo account preservation, noninteractive/check/pending/error exits')
