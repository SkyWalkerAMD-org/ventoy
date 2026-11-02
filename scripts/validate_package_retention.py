"""Real HTTP -> retained DEB/RPM -> native installation in disposable roots.

Usage: python3 validate_package_retention.py SOFTWARE_PY [PREBUILT_FIXTURE] [--policy POLICY_SH]
The optional fixture is a ventoy-qa-retention 1.0 package, built in another
disposable root when the target image has no package-building tools.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

sys.dont_write_bytecode = True
assert os.geteuid() == 0 and Path('/run/ventoy-policy-disposable-root').is_file(), 'Disposable root marker required'
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('software', type=Path)
parser.add_argument('fixture', nargs='?', type=Path)
parser.add_argument('--policy', type=Path, help='real policy checker when this disposable root already has a kernel policy')
args = parser.parse_args()
source = args.software
if Path('/var/lib/ventoy-center/kernel-policy').is_dir():
    assert args.policy and args.policy.is_file(), 'Existing kernel policy requires its real --policy checker'
spec = importlib.util.spec_from_file_location('software', source)
client = importlib.util.module_from_spec(spec)
spec.loader.exec_module(client)
target = client.target()
family = 'deb' if target['os'] in ('ubuntu', 'debian') else 'rpm'
name = 'ventoy-qa-retention'
assert not any(k.split(':')[0] == name for k in client.installed(family)), 'Use a fresh disposable root'


def build_fixture(root, package_name=name, copy_only=False):
    root.mkdir(parents=True, exist_ok=True)
    # A copy-only fixture's maintainer script MUST never execute.
    name = package_name
    if family == 'deb':
        package = root/'build'
        (package/'DEBIAN').mkdir(parents=True)
        (package/'DEBIAN/control').write_text('Package: '+name+'\nVersion: 1.0\nArchitecture: all\nMaintainer: QA <qa@example.invalid>\nDescription: package retention fixture\n')
        if copy_only:
            post = package/'DEBIAN/postinst'
            post.write_text('#!/bin/sh\necho COPY_ONLY_SCRIPT_MUST_NOT_RUN >&2\nexit 42\n')
            post.chmod(0o755)
        content = package/'usr/share'/name
        content.mkdir(parents=True)
        (content/'payload').write_text('retention fixture\n')
        result = root/(name+'_1.0_all.deb')
        subprocess.run(['dpkg-deb', '--build', str(package), str(result)], check=True)
        return result
    spec = root/'retention.spec'
    spec.write_text('Name: '+name+'\nVersion: 1.0\nRelease: 1\nSummary: retention fixture\nLicense: MIT\nBuildArch: noarch\n%description\nretention fixture\n%install\nmkdir -p %{buildroot}/usr/share/'+name+'\necho retained > %{buildroot}/usr/share/'+name+'/payload\n%files\n/usr/share/'+name+'\n' + ('%post\necho COPY_ONLY_SCRIPT_MUST_NOT_RUN >&2\nexit 42\n' if copy_only else ''))
    subprocess.run(['rpmbuild', '-bb', '--define', '_topdir '+str(root/'rpm'), str(spec)], check=True)
    return next((root/'rpm/RPMS/noarch').glob('*.rpm'))


with tempfile.TemporaryDirectory(prefix='vc-retention-') as folder:
    root = Path(folder)
    package = args.fixture if args.fixture else build_fixture(root)
    # CI runs policy validation before retention in the same disposable root.
    # Preserve the real post-install policy check when relocating client.HERE.
    if args.policy:
        shutil.copyfile(args.policy, root/'linux-update-policy.sh')
    payload = package.read_bytes()
    group_id = '/'.join([target['os'], target['version'], target['arch'], 'default'])
    item = dict(path=group_id+'/'+package.name, size=len(payload), sha256=hashlib.sha256(payload).hexdigest())
    manifest = dict(schema=1, target=target, group=dict(id=group_id, files=[item], **target))
    payloads = {package.name: payload}
    if not args.fixture:
        for index, excluded in enumerate(client.COPY_ONLY_PACKAGES):
            assert not any(k.split(':')[0] == excluded for k in client.installed(family)), 'Excluded package must start absent'
            data = build_fixture(root/('copy-'+str(index)), excluded, copy_only=True).read_bytes()
            # Real metadata must win over a filename without the sckoct prefix.
            filename = 'retained-%d.%s' % (index, family)
            payloads[filename] = data
            manifest['group']['files'].append(dict(path=group_id+'/'+filename, size=len(data), sha256=hashlib.sha256(data).hexdigest()))
    initial_downloads = len(payloads)
    downloads = []
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            assert self.headers['Authorization'] == 'Bearer '+'a'*64
            if self.path.startswith('/software/v1/manifest?'):
                data = json.dumps(manifest).encode()
            elif self.path.startswith('/software/v1/package?'):
                from urllib.parse import parse_qs, urlsplit
                downloads.append(self.path)
                data = payloads[parse_qs(urlsplit(self.path).query)['path'][0].rsplit('/', 1)[-1]]
            else:
                self.send_error(404); return
            self.send_response(200); self.end_headers(); self.wfile.write(data)
        def log_message(self, *args): pass
    server = HTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
    try:
        client.HERE, client.ARCHIVES, client.STATE, client.STAGING = str(root), str(root/'copies'), str(root/'state'), str(root)
        (root/'software-client.json').write_text(json.dumps(dict(enabled=True, url='http://127.0.0.1:'+str(server.server_port), token='a'*64)))
        client.execute('--software')
        dest = root/'copies'/package.name
        assert dest.read_bytes() == payload and dest.stat().st_mode & 0o777 == 0o600
        record = root/'state/last-result.json'
        assert json.loads(record.read_text())['changed'] == 1
        assert len(downloads) == initial_downloads
        assert len(json.loads(record.read_text())['copy_only_packages']) == initial_downloads-1
        for excluded in client.COPY_ONLY_PACKAGES:
            assert not any(k.split(':')[0] == excluded for k in client.installed(family)), 'Copy-only package was installed'
        for filename, data in payloads.items():
            assert (root/'copies'/filename).read_bytes() == data
        client.execute('--check-software')
        client.execute('--software')
        assert len(downloads) == initial_downloads and json.loads(record.read_text())['changed'] == 0
        dest.unlink()
        try: client.execute('--check-software')
        except RuntimeError: pass
        else: raise AssertionError('missing package not detected')
        client.execute('--software')
        assert dest.read_bytes() == payload and len(downloads) == initial_downloads+1
        assert json.loads(record.read_text())['changed'] == 0
        dest.write_bytes(b'operator modified')
        try: client.execute('--software')
        except RuntimeError: pass
        else: raise AssertionError('modified file overwritten')
        assert dest.read_bytes() == b'operator modified'
        assert json.loads(record.read_text())['status'] == 'failed'
        print('NATIVE_RETENTION_PASS', family, 'HTTP download, native install, copy-only metadata exclusion without running maintainer scripts, retained bytes, idempotence, missing-file backfill without reinstall, changed-file protection')
    finally:
        server.shutdown(); server.server_close(); thread.join()
