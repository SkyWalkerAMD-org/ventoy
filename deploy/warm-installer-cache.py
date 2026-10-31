#!/usr/bin/env python3
"""Warm signed APT dependencies inside an explicitly disposable apt root.

The root must contain apt-get and the requested distribution's official keyring.
Nothing is installed and no host APT configuration is changed. A successful
warm-up is a dated observation, not a promise that future metadata is cached.
"""
import argparse
import datetime
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
from urllib.parse import urlsplit


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--apt-root', required=True)
    p.add_argument('--origin', choices=['ubuntu', 'ubuntu-archive', 'debian', 'debian-archive'], required=True)
    p.add_argument('--suite', required=True)
    p.add_argument('--keyring', required=True, help='Official keyring path inside the disposable root')
    p.add_argument('--cache-url', required=True, help='Example: http://192.168.1.2:26081/installer-cache')
    p.add_argument('--output', required=True)
    a = p.parse_args()
    root = Path(a.apt_root).absolute()
    if root == Path('/') or root.resolve() != root or not (root/'.ventoy-cache-disposable').is_file():
        raise ValueError('Explicit disposable root marker required: .ventoy-cache-disposable')
    if not re.match(r'^[a-z][a-z0-9-]{1,30}$', a.suite):
        raise ValueError('Invalid suite')
    key = root / a.keyring.lstrip('/')
    if not a.keyring.startswith('/') or root not in key.resolve().parents or not key.is_file() or re.search(r'[\s\[\]]', a.keyring):
        raise ValueError('Invalid keyring')
    url = urlsplit(a.cache_url)
    if re.search(r'[\s\[\]";]', a.cache_url) or url.scheme not in ('http', 'https') or not url.hostname or url.username or url.password or url.query or url.fragment or url.path != '/installer-cache':
        raise ValueError('Invalid cache URL')
    work = Path(tempfile.mkdtemp(prefix='ventoy-warm-', dir=str(root/'var/tmp')))
    inside = '/' + str(work.relative_to(root))
    for folder in ('parts', 'lists/partial', 'archives/partial'):
        (work/folder).mkdir(parents=True)
    (work/'status').write_text('')
    # APT_CONFIG is read before apt.conf.d; suppress host-root hooks early.
    (work/'empty.conf').write_text('Dir::Etc::parts "%s/parts";\nDir::Etc::main "%s/no-main.conf";\n' % (inside, inside))
    components = 'main universe' if a.origin.startswith('ubuntu') else 'main'
    (work/'sources.list').write_text('deb [arch=amd64 signed-by=%s] %s/%s %s %s\n' % (a.keyring, a.cache_url, a.origin, a.suite, components))
    options = ['-o', 'Dir::Etc::main='+inside+'/empty.conf', '-o', 'Dir::Etc::parts='+inside+'/parts',
               '-o', 'Dir::Etc::sourcelist='+inside+'/sources.list', '-o', 'Dir::Etc::sourceparts='+inside+'/parts',
               '-o', 'Dir::State::lists='+inside+'/lists', '-o', 'Dir::State::status='+inside+'/status',
               '-o', 'Dir::Cache::archives='+inside+'/archives', '-o', 'APT::Architecture=amd64',
               '-o', 'Acquire::Retries=3', '-o', 'Acquire::http::Timeout=1200',
               '-o', 'Acquire::AllowInsecureRepositories=false', '-o', 'APT::Get::AllowUnauthenticated=false']
    packages = ['openssh-server', 'grub-efi-amd64', 'efibootmgr']
    evidence = dict(schema=1, origin=a.origin, suite=a.suite, architecture='amd64', packages=packages,
                    time=datetime.datetime.utcnow().isoformat()+'Z', status='failed', signatures='APT official keyring verification')
    try:
        with (work/'apt.log').open('wb') as log:
            base = ['chroot', str(root), '/usr/bin/env', 'APT_CONFIG='+inside+'/empty.conf', 'DEBIAN_FRONTEND=noninteractive', 'apt-get'] + options
            subprocess.run(base+['update', '-o', 'APT::Update::Error-Mode=any'], stdout=log, stderr=subprocess.STDOUT, check=True, timeout=3600)
            subprocess.run(base+['--no-install-recommends', '--download-only', '-y', 'install']+packages, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=3600)
        evidence.update(downloaded_packages=len(list((work/'archives').glob('*.deb'))))
        if evidence['downloaded_packages'] == 0:
            raise RuntimeError('No package downloads verified')
        evidence['status'] = 'verified'
    finally:
        evidence['log'] = str(work/'apt.log')
        out = Path(a.output)
        out.parent.mkdir(parents=True, exist_ok=True)
        temporary = out.with_suffix('.next')
        with temporary.open('x') as f:
            json.dump(evidence, f, indent=2)
        os.replace(str(temporary), str(out))
        print(json.dumps(evidence))


if __name__ == '__main__':
    main()
