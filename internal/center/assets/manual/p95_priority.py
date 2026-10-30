"""Install only the release-bundled independent P95 helper; Python 2.7/3.x."""
from __future__ import print_function
import hashlib
import os
import platform
import re
import stat
import subprocess

HERE = os.path.dirname(os.path.realpath(__file__))
SETUP = os.path.join(HERE, 'p95-priority-setup.sh')
SETUP_SHA256 = '70709eb5bef6d4067892bf8ef034a05d3c42f55d8ed53ea4d8ebff27bb434ab2'
VERSION = '1.0.2'
PRODUCTS = ('fyhocsys', 'sckocstp')


def command(args, echo=False):
    p = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                         env=dict(os.environ, LC_ALL='C'))
    output = p.communicate()[0].decode('utf-8', 'replace')
    if echo:
        print(output.rstrip())
    if p.returncode:
        raise RuntimeError('P95 priority helper failed (exit %s): %s' % (p.returncode, output[-10000:]))
    return output.strip()


def verify_setup():
    info = os.lstat(SETUP)
    if not stat.S_ISREG(info.st_mode):
        raise RuntimeError('P95 helper must be the bundled regular file')
    digest = hashlib.sha256()
    with open(SETUP, 'rb') as source:
        for chunk in iter(lambda: source.read(1024*1024), b''):
            digest.update(chunk)
    if digest.hexdigest() != SETUP_SHA256:
        raise RuntimeError('Bundled P95 helper checksum mismatch; nothing executed')


def configure(installed_names):
    products = [name for name in PRODUCTS if name+'-node' in installed_names]
    if not products:
        print('P95_PRIORITY_SKIPPED: no fyhocsys/sckocstp node package is installed.')
        return []
    reason = ''
    if platform.machine().lower() not in ('x86_64', 'amd64'):
        reason = 'helper supports Linux x86_64 only'
    else:
        output = command(['systemctl', '--version'])
        match = re.match(r'^systemd\s+([0-9]+)', output)
        if not match:
            raise RuntimeError('Cannot determine systemd version for P95 helper')
        if int(match.group(1)) < 219:
            reason = 'systemd 219+ required'
    if reason:
        print('P95_PRIORITY_SKIPPED: '+reason)
        return [dict(program=name, status='skipped', reason=reason) for name in products]
    verify_setup()
    records = []
    for product in products:
        command(['bash', SETUP, '--product', product], echo=True)
        command(['bash', SETUP, '--check', '--product', product], echo=True)
        records.append(dict(program=product, status='verified', version=VERSION))
    return records


def check(records):
    if not isinstance(records, list):
        raise RuntimeError('Invalid P95 helper verification record')
    for record in records:
        product = record.get('program')
        if product not in PRODUCTS:
            raise RuntimeError('Invalid P95 helper product')
        if record.get('status') == 'skipped':
            print('P95_PRIORITY_SKIPPED: %s: %s' % (product, record.get('reason', 'unsupported host')))
            continue
        if record.get('status') != 'verified' or record.get('version') != VERSION:
            raise RuntimeError('P95 helper release changed; run setup --software to apply it')
        verify_setup()
        command(['bash', SETUP, '--check', '--product', product], echo=True)
