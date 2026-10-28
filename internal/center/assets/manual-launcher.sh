#!/usr/bin/env bash
# Ventoy Center complete manual-install configuration package.
set -Eeuo pipefail
umask 077
if (($# == 0)); then set -- --apply; fi
if [[ ${1:---help} == --help || ${1:-} == -h ]]; then
    echo 'Usage: sudo ./ventoy-linux-setup.run [--hostname NAME] [--offline] [--keep-passwords]'
    echo 'Run after installing and booting Linux. Default credentials: user / ventoy, root / ventoy.'
    echo 'No arguments applies and verifies SSH, hostname, DHCP, kernel locking, disabled automatic updates, and matching software.'
    echo 'Interactive full setup asks for hostname and an optional new root password (twice, hidden); Enter keeps the current value.'
    echo '--apply remains a compatible alias; --offline skips software downloads.'
    echo 'Software only: sudo ./ventoy-linux-setup.run --software'
    echo 'Inspect/verify: --check / --list-software / --check-software'
    exit 0
fi
[[ $EUID == 0 ]] || { echo 'Run with root or sudo.' >&2; exit 1; }
hostname_before=$(hostname)
refresh_prompt=1
case "${1:-}" in --check|--resume|--software|--list-software|--check-software) refresh_prompt=0;; esac
python=
for candidate in python3 /usr/libexec/platform-python python2 python; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'import sys; sys.exit(0 if sys.version_info >= (2,7) else 1)' 2>/dev/null; then
        python=$candidate; break
    fi
done
[[ -n $python ]] || { echo 'Python >=2.7 is required. Install Python from the OS media, then rerun.' >&2; exit 1; }
stage=$(mktemp -d /var/tmp/ventoy-manual.XXXXXXXX)
trap 'rm -rf -- "$stage"' EXIT
"$python" - "$0" "$stage" '@ARCHIVE_SHA256@' <<'PY'
from __future__ import print_function
import hashlib, io, os, shutil, sys, tarfile
with open(sys.argv[1], 'rb') as f:
    while True:
        line=f.readline()
        if not line: raise ValueError('Missing archive marker')
        if line == b'__VENTOY_MANUAL_ARCHIVE__\n': break
    data=f.read(64*1024*1024+1)
if len(data)>64*1024*1024 or hashlib.sha256(data).hexdigest()!=sys.argv[3]:
    raise ValueError('Package checksum mismatch')
root=sys.argv[2]; total=0; seen=set()
with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
    for member in archive:
        name=member.name
        if (not member.isfile() or not name.startswith('ventoy-manual-setup/')
                or '\\' in name or any(p in ('','.','..') for p in name.split('/'))
                or name in seen or member.size<0 or member.size>64*1024*1024):
            raise ValueError('Unsafe archive member')
        seen.add(name); total+=member.size
        if total>256*1024*1024: raise ValueError('Archive too large')
        target=os.path.join(root,name); parent=os.path.dirname(target)
        if not os.path.isdir(parent): os.makedirs(parent)
        with open(target,'wb') as out: shutil.copyfileobj(archive.extractfile(member),out)
        os.chmod(target,0o755 if name.endswith(('.sh','.run')) else 0o644)
PY
setup_rc=0
bash "$stage/ventoy-manual-setup/install.sh" "$@" || setup_rc=$?
if (( (setup_rc == 0 || setup_rc == 3 || setup_rc == 4) && refresh_prompt)) && [[ -t 0 && -t 1 && -t 2 ]]; then
    # Status 4 means system checks reached the software step, which failed.
    # That independent failure must not hide the optional password dialog.
    password_rc=0
    bash "$stage/ventoy-manual-setup/root-password.sh" || password_rc=$?
    if ((password_rc && setup_rc != 4)); then setup_rc=$password_rc; fi
fi
# A child cannot replace its caller's cached Bash hostname. After a verified
# interactive rename, use a fresh shell on this same terminal. Never put a
# background/service/check invocation into an interactive shell, and clean the
# extraction directory before opening the refreshed shell.
hostname_after=$(hostname)
rm -rf -- "$stage"
trap - EXIT
if (((setup_rc == 0 || setup_rc == 3 || setup_rc == 4) && refresh_prompt)) && [[ -t 0 && -t 1 && -t 2 &&
        "$hostname_before" != "$hostname_after" ]]; then
    pgid=$(ps -o pgid= -p "$$" | tr -d ' ')
    foreground=$(ps -o tpgid= -p "$$" | tr -d ' ')
    if [[ -n "$pgid" && "$pgid" == "$foreground" ]]; then
        echo "HOSTNAME_VERIFIED: $hostname_after; opening a refreshed interactive Bash prompt."
        if ((setup_rc)); then
            echo "SETUP_INCOMPLETE: exit status $setup_rc is retained; refreshing the prompt does not resolve the errors above." >&2
        fi
        parent_command=$(ps -o comm= -p "$PPID" | tr -d ' ')
        parent_uid=$(ps -o ruid= -p "$PPID" | tr -d ' ')
        if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != root &&
                ( "$parent_command" == sudo || "$parent_uid" == "${SUDO_UID:-}" ) ]]; then
            # A sudo invocation must return to the invoking account, not leave
            # an unexpected root shell open. Verify sudo's recorded identity.
            if [[ $(id -u "$SUDO_USER") == "${SUDO_UID:-}" ]] && command -v runuser >/dev/null 2>&1; then
                runuser -u "$SUDO_USER" -- /bin/bash -i || true
                exit "$setup_rc"
            fi
            echo 'Hostname is active. Run exec bash in your current account to refresh its prompt.'
        else
            /bin/bash -i || true
            exit "$setup_rc"
        fi
    fi
fi
exit "$setup_rc"
__VENTOY_MANUAL_ARCHIVE__
