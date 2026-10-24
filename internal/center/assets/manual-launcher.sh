#!/usr/bin/env bash
# Ventoy Center complete manual-install configuration package.
set -Eeuo pipefail
umask 077
if [[ ${1:---help} == --help || ${1:-} == -h ]]; then
    echo 'Usage: sudo bash ventoy-linux-setup.run --apply [--hostname NAME] [--offline] [--keep-passwords]'
    echo 'Run after installing and booting Linux. Default credentials: admin / ventoy, root / ventoy.'
    exit 0
fi
[[ $EUID == 0 ]] || { echo 'Run with root or sudo.' >&2; exit 1; }
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
bash "$stage/ventoy-manual-setup/install.sh" "$@"
exit $?
__VENTOY_MANUAL_ARCHIVE__
