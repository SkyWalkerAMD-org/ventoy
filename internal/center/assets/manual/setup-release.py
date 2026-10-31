#!/usr/bin/env python
from __future__ import print_function
import hashlib
import json
import os
import re
import stat
import sys
import tempfile
from software import Client, HERE


def execute(action, destination):
    if action not in ('--check-version', '--update-setup'):
        raise ValueError('Unknown setup release action')
    with open(os.path.join(HERE, 'software-client.json')) as f:
        config = json.load(f)
    if not config.get('enabled'):
        raise RuntimeError('Central setup distribution is not configured')
    client = Client(config)
    response = client.get('setup-release', {})
    try:
        raw = response.read(16385)
    finally:
        response.close()
    if len(raw) > 16384:
        raise RuntimeError('Release metadata too large')
    release = json.loads(raw.decode('utf-8'))
    if not re.match(r'^[a-f0-9]{64}$', release['sha256']) or not 0 < release['bytes'] <= 64*1024*1024:
        raise RuntimeError('Invalid setup release metadata')
    with open(os.path.join(HERE, 'VERSION')) as f:
        current = f.read().strip()
    print('SETUP_VERSION: local=%s center=%s' % (current, release['version']))
    if action == '--check-version':
        print('Use --update-setup to download, verify and replace this runner. No automatic execution.')
        return
    destination = os.path.abspath(destination)
    if os.path.realpath(destination) != destination:
        raise RuntimeError('Refusing a symlink setup destination')
    before = os.stat(destination)
    if not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_mode & 0o022:
        raise RuntimeError('Setup file must be root-owned and not writable by other users')
    with open(destination, 'rb') as f:
        original = f.read(64*1024*1024+1)
    if len(original) > 64*1024*1024:
        raise RuntimeError('Original runner exceeds size limit')
    if hashlib.sha256(original).hexdigest() == release['sha256']:
        print('SETUP_CURRENT: this file already matches the center')
        return
    fd, temporary = tempfile.mkstemp(prefix='.ventoy-setup-', dir=os.path.dirname(destination))
    try:
        h, size = hashlib.sha256(), 0
        with os.fdopen(fd, 'wb') as f:
            response = client.get('setup-release/download', {})
            try:
                while True:
                    block = response.read(1024*1024)
                    if not block:
                        break
                    size += len(block)
                    if size > release['bytes']:
                        raise RuntimeError('Setup size changed during download')
                    h.update(block)
                    f.write(block)
                f.flush()
                os.fsync(f.fileno())
            finally:
                response.close()
        if size != release['bytes'] or h.hexdigest() != release['sha256']:
            raise RuntimeError('Setup checksum mismatch; original retained')
        # A concurrently replaced file must not be overwritten.
        after = os.stat(destination)
        fields = ('st_dev', 'st_ino', 'st_size', 'st_mtime', 'st_mode', 'st_uid', 'st_gid')
        if os.path.realpath(destination) != destination or any(getattr(after, k) != getattr(before, k) for k in fields):
            raise RuntimeError('Setup file changed while downloading')
        with open(destination, 'rb') as f:
            if f.read() != original:
                raise RuntimeError('Setup changed while downloading')
        backup = destination+'.previous-'+hashlib.sha256(original).hexdigest()[:12]
        if not os.path.lexists(backup):
            with os.fdopen(os.open(backup, os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600), 'wb') as f:
                f.write(original)
                f.flush()
                os.fsync(f.fileno())
        else:
            if os.path.islink(backup) or not os.path.isfile(backup) or os.stat(backup).st_uid != 0:
                raise RuntimeError('Unsafe previous-runner backup')
            with open(backup, 'rb') as f:
                if f.read(64*1024*1024+1) != original:
                    raise RuntimeError('Previous-runner backup differs from original')
        os.chmod(temporary, 0o700)
        os.rename(temporary, destination)
        print('SETUP_UPDATED: %s; previous copy %s; run setup again when ready.' % (release['version'], backup))
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


if __name__ == '__main__':
    try:
        execute(sys.argv[1], sys.argv[2])
    except Exception as e:
        print('SETUP_UPDATE_FAILED: '+str(e), file=sys.stderr)
        sys.exit(1)
