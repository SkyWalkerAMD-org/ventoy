#!/usr/bin/env bash
# One-time bridge for setup packages that predate --upgrade. Python 2.7.9+ / 3.x.
set -euo pipefail
python=
for candidate in python3 /usr/libexec/platform-python python2 python; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'import ssl; ssl.create_default_context()' >/dev/null 2>&1; then
        python=$candidate; break
    fi
done
[[ -n $python ]] || { echo 'Python with verified TLS support is required.' >&2; exit 1; }
exec "$python" - "$@" <<'VENTOY_UPGRADE_PYTHON'
from __future__ import print_function
import argparse
import hashlib
import io
import json
import os
import re
import ssl
import stat
import sys
import tarfile
import tempfile
try:
    from urllib.request import Request, build_opener, HTTPSHandler, HTTPRedirectHandler
    from urllib.parse import urlsplit
except ImportError:
    from urllib2 import Request, build_opener, HTTPSHandler, HTTPRedirectHandler
    from urlparse import urlsplit

LIMIT = 64 * 1024 * 1024
MARKER = b'\n__VENTOY_MANUAL_ARCHIVE__\n'


def owned_file(path):
    if os.path.realpath(path) != path:
        raise ValueError('Refusing a symlink path: ' + path)
    st = os.stat(path)
    if not stat.S_ISREG(st.st_mode) or st.st_uid != 0 or st.st_mode & 0o022:
        raise ValueError('File must be root-owned and not writable by other users: ' + path)
    return st


def payload(raw):
    if len(raw) > LIMIT or MARKER not in raw:
        raise ValueError('Not a bounded Ventoy setup runner')
    header, data = raw.split(MARKER, 1)
    # Verify the payload against its launcher, without executing legacy code.
    digest = hashlib.sha256(data).hexdigest().encode('ascii')
    if not re.search(b"['\"]" + digest + b"['\"]", header):
        raise ValueError('Legacy setup payload checksum mismatch')
    wanted = {'VERSION': 256, 'software-client.json': 16384, 'software-ca.crt': 65536}
    found, total = {}, 0
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
        for index, member in enumerate(archive):
            if index > 4096:
                raise ValueError('Too many setup archive members')
            total += member.size
            if member.size < 0 or member.size > LIMIT or total > 256 * 1024 * 1024:
                raise ValueError('Setup archive exceeds its expanded size limit')
            prefix = 'ventoy-manual-setup/'
            name = member.name[len(prefix):] if member.name.startswith(prefix) else ''
            if name not in wanted:
                continue
            if name in found or not member.isfile() or not 0 < member.size <= wanted[name]:
                raise ValueError('Invalid setup trust/configuration member')
            found[name] = archive.extractfile(member).read()
    if 'software-client.json' not in found:
        raise ValueError('Legacy package has no center configuration; download a current setup from the management page once')
    return header, found


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError('Center redirect refused')


def transport(files, ca_path):
    config = json.loads(files['software-client.json'].decode('utf-8'))
    if not config.get('enabled'):
        raise ValueError('Legacy package has no enabled center distribution')
    origin = config['url'].rstrip('/')
    url = urlsplit(origin)
    if url.scheme != 'https' or not url.hostname or url.username or url.password or url.path or url.query or url.fragment:
        raise ValueError('Center must be an HTTPS origin without credentials')
    token = config.get('token', '')
    if not re.match(r'^[a-f0-9]{64}\Z', token):
        raise ValueError('Invalid center download credential')
    ca = config.get('ca_file')
    if ca not in (None, '', 'software-ca.crt'):
        raise ValueError('Invalid center certificate path')
    if ca:
        if 'software-ca.crt' not in files:
            raise ValueError('Legacy package is missing its trusted center certificate')
        with open(ca_path, 'wb') as f:
            f.write(files['software-ca.crt'])
    context = ssl.create_default_context(cafile=ca_path if ca else None)
    opener = build_opener(NoRedirect(), HTTPSHandler(context=context))
    def get(endpoint):
        request = Request(origin + '/software/v1/' + endpoint,
                          headers={'Authorization': 'Bearer ' + token})
        return opener.open(request, timeout=60)
    return get


def same_file(path, before, raw):
    after = owned_file(path)
    fields = ('st_dev', 'st_ino', 'st_size', 'st_mtime', 'st_mode', 'st_uid', 'st_gid')
    if any(getattr(after, k) != getattr(before, k) for k in fields):
        raise ValueError('Setup changed during upgrade; original retained')
    with open(path, 'rb') as f:
        if f.read(LIMIT + 1) != raw:
            raise ValueError('Setup content changed during upgrade; original retained')


def update(runner, check_only=False):
    runner = os.path.abspath(runner)
    before = owned_file(runner)
    with open(runner, 'rb') as f:
        old = f.read(LIMIT + 1)
    header, files = payload(old)
    sidecar = runner + '.sha256'
    if os.path.lexists(sidecar):
        owned_file(sidecar)
    fd, ca = tempfile.mkstemp(prefix='.ventoy-upgrade-ca-', dir=os.path.dirname(runner))
    os.close(fd)
    temporary = None
    try:
        get = transport(files, ca)
        response = get('setup-release')
        try:
            metadata = response.read(16385)
        finally:
            response.close()
        if len(metadata) > 16384:
            raise ValueError('Release metadata too large')
        release = json.loads(metadata.decode('utf-8'))
        if (not re.match(r'^[a-f0-9]{64}\Z', release['sha256']) or
                not isinstance(release['bytes'], (int, type(LIMIT * LIMIT))) or
                not 0 < release['bytes'] <= LIMIT):
            raise ValueError('Invalid center release metadata')
        print('SETUP_VERSION: local=%s center=%s' % (files.get('VERSION', b'unknown').decode().strip(), release['version']))
        if check_only:
            return
        old_hash = hashlib.sha256(old).hexdigest()
        if old_hash != release['sha256']:
            fd, temporary = tempfile.mkstemp(prefix='.ventoy-upgrade-', dir=os.path.dirname(runner))
            digest, size = hashlib.sha256(), 0
            with os.fdopen(fd, 'wb') as f:
                response = get('setup-release/download')
                try:
                    while True:
                        block = response.read(1024 * 1024)
                        if not block:
                            break
                        size += len(block)
                        if size > release['bytes']:
                            raise ValueError('Setup size changed during download')
                        digest.update(block)
                        f.write(block)
                    f.flush()
                    os.fsync(f.fileno())
                finally:
                    response.close()
            if size != release['bytes'] or digest.hexdigest() != release['sha256']:
                raise ValueError('Setup checksum mismatch; original retained')
            with open(temporary, 'rb') as f:
                new_header, new_files = payload(f.read())
            if b'--upgrade' not in new_header or new_files.get('VERSION', b'').decode().strip() != release['version']:
                raise ValueError('Center package does not support upgrade or has a different version')
            same_file(runner, before, old)
            backup = runner + '.previous-' + old_hash[:12]
            if os.path.lexists(backup):
                owned_file(backup)
                with open(backup, 'rb') as f:
                    if f.read(LIMIT + 1) != old:
                        raise ValueError('Previous-runner backup differs from original')
            else:
                with os.fdopen(os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'wb') as f:
                    f.write(old)
                    f.flush()
                    os.fsync(f.fileno())
            os.chmod(temporary, 0o700)
            os.rename(temporary, runner)
            temporary = None
            print('SETUP_UPDATED: %s; previous file retained at %s' % (release['version'], backup))
        else:
            if b'--upgrade' not in header:
                raise ValueError('Center still serves a legacy runner without upgrade support')
            same_file(runner, before, old)
            os.chmod(runner, 0o700)
            print('SETUP_CURRENT: executable permission repaired')
        fd, temporary = tempfile.mkstemp(prefix='.ventoy-checksum-', dir=os.path.dirname(runner))
        with os.fdopen(fd, 'wb') as f:
            f.write(('%s  %s\n' % (release['sha256'], os.path.basename(runner))).encode('utf-8'))
            f.flush()
            os.fsync(f.fileno())
        os.rename(temporary, sidecar)
        temporary = None
    finally:
        for path in (ca, temporary):
            if path and os.path.exists(path):
                os.unlink(path)


def main():
    parser = argparse.ArgumentParser(description='Upgrade legacy Ventoy setup using its existing center HTTPS trust. Hostname/passwords are retained.')
    parser.add_argument('--runner', default='./ventoy-linux-setup.run')
    actions = parser.add_mutually_exclusive_group()
    actions.add_argument('--update-setup', action='store_true', help='Download/repair permissions only; do not apply system changes')
    actions.add_argument('--check-version', action='store_true', help='Read the center version without changing the runner')
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise ValueError('Run with root or sudo')
    os.umask(0o077)
    runner = os.path.abspath(args.runner)
    update(runner, args.check_version)
    if not args.check_version and not args.update_setup:
        print('SETUP_UPGRADE: applying current setup; existing passwords are retained')
        sys.stdout.flush()
        # Reconnect stdin to the terminal: Python itself was read from a heredoc.
        # Without this, hostname/password dialogs would silently be non-interactive.
        if sys.stdout.isatty() and sys.stderr.isatty():
            try:
                fd = os.open('/dev/tty', os.O_RDONLY)
                os.dup2(fd, 0)
                os.close(fd)
            except OSError:
                pass
        os.execv('/bin/bash', ['/bin/bash', runner, '--upgrade', '--keep-passwords'])


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('SETUP_UPGRADE_FAILED: ' + str(error), file=sys.stderr)
        sys.exit(1)
VENTOY_UPGRADE_PYTHON
