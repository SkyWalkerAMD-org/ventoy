"""Software-client boundaries; --native exercises real disposable RPM/DEB roots."""
import copy
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
import unittest
from unittest.mock import patch
from http.server import BaseHTTPRequestHandler, HTTPServer

sys.dont_write_bytecode = True
source = Path(sys.argv.pop()) if len(sys.argv) > 1 and sys.argv[-1].endswith('.py') else Path(__file__).resolve().parents[1] / 'internal/center/assets/manual/software.py'
spec = importlib.util.spec_from_file_location('software', source)
client = importlib.util.module_from_spec(spec)
spec.loader.exec_module(client)


class Boundaries(unittest.TestCase):
    def test_copy_file_manifest(self):
        wanted = dict(os='ubuntu', version='22.04', arch='amd64')
        item = dict(path='centos_opt_static', size=10, sha256='0'*64)
        manifest = dict(schema=1, target=wanted, group=None, copy_files=[item])
        client.validate_manifest(manifest, wanted)
        for name in ('ubuntu-optimize.sh.x', 'driver.tar.xz', 'README.txt'):
            good = copy.deepcopy(manifest); good['copy_files'][0]['path'] = name
            client.validate_manifest(good, wanted)
        for name in ('../tool', 'dir/tool', 'tool\\x', '.hidden', 'tool\n', 'app.deb', 'app.rpm',
                     'onload.tgz', 'app.tar.gz', 'app.zip', 'tool.part', 'tool.partial',
                     'tool.tmp', 'tool.download', 'tool.crdownload', 'tool.swp', 'tool.bak', 'tool~',
                     'ventoy-linux-setup.run', 'ventoy-linux-setup.run.sha256'):
            bad = copy.deepcopy(manifest); bad['copy_files'][0]['path'] = name
            with self.assertRaises(RuntimeError): client.validate_manifest(bad, wanted)
        for count in (2, 257):
            bad = copy.deepcopy(manifest); bad['copy_files'] *= count
            with self.assertRaises(RuntimeError): client.validate_manifest(bad, wanted)

    def test_archive_manifest(self):
        target = dict(os='ubuntu', version='24.04', arch='amd64')
        item = dict(path='ipmitool-1.8.19.tar.gz', size=10, sha256='0'*64)
        manifest = dict(schema=1, target=target, group=None, archives=[item])
        client.validate_manifest(manifest, target)
        for name in ('onload-7.1.3.202.tgz', 'onload-8.1.3.40.tgz', 'exanic-software.zip'):
            supported = copy.deepcopy(manifest); supported['archives'][0]['path'] = name
            client.validate_manifest(supported, target)
        for key, value in [('path', '../app.tar.gz'), ('path', 'nested/app.tar.gz'),
                           ('path', '../app.tgz'), ('path', 'nested/app.zip'),
                           ('path', 'app.tgz.part'), ('path', 'app.zip.part'),
                           ('path', 'app.zip\n'), ('path', 'app.tgz\n'),
                           ('path', 'app.deb'), ('path', 'app.tar.gz\n'), ('size', 0),
                           ('size', 2**40), ('sha256', 'bad')]:
            bad = copy.deepcopy(manifest); bad['archives'][0][key] = value
            with self.assertRaises(RuntimeError): client.validate_manifest(bad, target)
        bad = copy.deepcopy(manifest); bad['archives'] *= 2
        with self.assertRaises(RuntimeError): client.validate_manifest(bad, target)

    @unittest.skipIf(sys.platform == 'win32', 'Linux file ownership and atomic replacement')
    def test_archive_transfer_repeat_replace_and_protection(self):
        # Includes a path-traversing tar member; delivery must preserve the raw
        # bytes without ever extracting or executing anything from the archive.
        import io, tarfile
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode='w:gz') as t:
            entry = tarfile.TarInfo('../../must-not-be-extracted')
            entry.size = 4; t.addfile(entry, io.BytesIO(b'data'))
        payload = stream.getvalue()
        class FakeClient:
            calls = 0
            fail = False
            def download(self, item, dest):
                self.calls += 1
                Path(dest).write_bytes(payload)
                if self.fail: raise RuntimeError('transfer interrupted')
        api = FakeClient()
        def item(data): return dict(path='ipmitool.tar.gz', size=len(data), sha256=hashlib.sha256(data).hexdigest())
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with patch.object(client, 'ARCHIVES', str(root/'files')), patch.object(client, 'STATE', str(root/'state')):
                client.sync_archives(api, [item(payload)])
                dest = root/'files/ipmitool.tar.gz'
                self.assertEqual(dest.read_bytes(), payload)
                self.assertEqual(dest.stat().st_mode & 0o777, 0o600)
                self.assertEqual(sorted(x.name for x in (root/'files').iterdir()), ['ipmitool.tar.gz'])
                client.verify_archives([item(payload)])
                client.sync_archives(api, [item(payload)])
                self.assertEqual(api.calls, 1)
                old = payload; payload = b'new raw archive bytes'
                api.fail = True
                with self.assertRaises(RuntimeError): client.sync_archives(api, [item(payload)])
                self.assertEqual(dest.read_bytes(), old)
                api.fail = False
                client.sync_archives(api, [item(payload)])
                self.assertEqual(dest.read_bytes(), payload)
                dest.write_bytes(b'user-modified')
                with self.assertRaises(RuntimeError): client.verify_archives([item(payload)])
                with self.assertRaises(RuntimeError): client.sync_archives(api, [item(payload)])
                self.assertEqual(dest.read_bytes(), b'user-modified')
                dest.unlink(); outside=root/'outside'; outside.write_bytes(payload); dest.symlink_to(outside)
                with self.assertRaises(RuntimeError): client.sync_archives(api, [item(payload)])
                self.assertEqual(outside.read_bytes(), payload)
                dest.unlink(); (root/'files').rmdir(); (root/'files').symlink_to(root, target_is_directory=True)
                with self.assertRaises(RuntimeError): client.sync_archives(api, [item(payload)])

    @unittest.skipIf(sys.platform == 'win32', 'Linux filesystem delivery')
    def test_archive_only_execution_and_readonly_check(self):
        wanted = dict(os='ubuntu', version='24.04', arch='amd64')
        payload = b'archive-only'
        archives = [dict(path=name, size=len(payload), sha256=hashlib.sha256(payload).hexdigest())
                    for name in ('tools.tar.gz', 'onload.tgz', 'exanic.zip')]
        copies = [dict(path=name, size=len(payload), sha256=hashlib.sha256(payload).hexdigest())
                  for name in ('centos_opt_static', 'ubuntu-optimize.sh.x')]
        manifest = dict(schema=1, target=wanted, group=None, archives=archives, copy_files=copies)
        class FakeClient:
            def __init__(self, config): pass
            def manifest(self, target): return manifest
            def download(self, item, dest): Path(dest).write_bytes(payload)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); (root/'software-client.json').write_text('{"enabled": true}')
            with patch.object(client, 'ARCHIVES', str(root/'files')), patch.object(client, 'STATE', str(root/'state')), \
                 patch.object(client, 'HERE', str(root)), patch.object(client, 'target', return_value=wanted), \
                 patch.object(client, 'Client', FakeClient), patch.object(client, 'installed', return_value={}), \
                 patch.object(client, 'run', side_effect=AssertionError('unexpected package command')):
                client.execute('--list-software')
                self.assertFalse((root/'files').exists())
                client.execute('--software')
                report = json.loads((root/'state/last-result.json').read_text())
                self.assertEqual((report['status'], report['packages'], report['archives']), ('verified', [], archives + copies))
                self.assertEqual(sorted(p.name for p in (root/'files').iterdir()), ['centos_opt_static', 'exanic.zip', 'onload.tgz', 'tools.tar.gz', 'ubuntu-optimize.sh.x'])
                client.execute('--check-software')
                client.execute('--software')
                manifest['archives'] = []
                client.execute('--software')
                client.execute('--check-software')
                (root/'files/ubuntu-optimize.sh.x').write_bytes(b'user changed tool')
                with self.assertRaises(RuntimeError): client.execute('--software')
                self.assertEqual((root/'files/ubuntu-optimize.sh.x').read_bytes(), b'user changed tool')
                (root/'files/ubuntu-optimize.sh.x').write_bytes(payload)
                manifest['archives'] = archives
                client.execute('--software')
                (root/'files/tools.tar.gz').unlink()
                with self.assertRaises(RuntimeError): client.execute('--check-software')

    def test_manifest_boundaries(self):
        target = dict(os='ubuntu', version='22.04', arch='amd64')
        group = dict(id='auto/ubuntu/22.04/amd64/default', **target, files=[dict(path='app_1_amd64.deb', size=10, sha256='0'*64)])
        manifest = dict(schema=1, target=target, group=group)
        client.validate_manifest(manifest, target)
        for key, value in [('path', '../app.deb'), ('path', '/etc/app.deb'), ('path', 'app.rpm'), ('size', 0), ('size', 2**40), ('sha256', 'wrong')]:
            bad = copy.deepcopy(manifest)
            bad['group']['files'][0][key] = value
            with self.assertRaises(RuntimeError): client.validate_manifest(bad, target)
        for key, value in [('os','debian'), ('version','24.04'), ('arch','arm64')]:
            bad = copy.deepcopy(manifest); bad['group'][key] = value
            with self.assertRaises(RuntimeError): client.validate_manifest(bad,target)
        bad = copy.deepcopy(manifest);bad['group']['files'] *= 2
        with self.assertRaises(RuntimeError): client.validate_manifest(bad,target)

    def test_transport_integrity_and_no_redirect(self):
        class Handler(BaseHTTPRequestHandler):
            redirect = False
            def do_GET(self):
                if self.headers.get('Authorization') != 'Bearer '+'a'*64:
                    self.send_error(401);return
                if self.redirect:
                    self.send_response(302); self.send_header('Location', 'http://example.invalid/secret');self.end_headers();return
                self.send_response(200); self.end_headers();self.wfile.write(b'content')
            def log_message(self,*args): pass
        server = HTTPServer(('127.0.0.1',0),Handler)
        threading.Thread(target=server.serve_forever,daemon=True).start()
        try:
            api = client.Client(dict(url='http://127.0.0.1:'+str(server.server_port),token='a'*64))
            with tempfile.TemporaryDirectory() as temp:
                item=dict(path='app_1_amd64.deb',size=7,sha256=hashlib.sha256(b'content').hexdigest())
                dest=str(Path(temp)/'package');api.download(item,dest)
                self.assertEqual(Path(dest).read_bytes(),b'content')
                item['sha256']='0'*64
                with self.assertRaises(RuntimeError):api.download(item,dest)
                item['size']=1
                with self.assertRaises(RuntimeError):api.download(item,dest)
                Handler.redirect=True
                with self.assertRaises(RuntimeError):api.download(item,dest)
            with self.assertRaises(RuntimeError):client.Client(dict(url='http://192.168.1.2',token='a'*64))
        finally:
            server.shutdown();server.server_close()


def native():
    assert os.geteuid()==0 and Path('/run/ventoy-policy-disposable-root').is_file(), 'Disposable container marker required'
    family='deb' if Path('/usr/bin/dpkg').is_file() else 'rpm'
    arch=client.target()['arch']
    def run(args): return subprocess.check_output(args,stderr=subprocess.STDOUT,universal_newlines=True)
    with tempfile.TemporaryDirectory() as temp:
        root=Path(temp)
        def package(name,dependency='',version='1'):
            if family=='deb':
                d=root/(name+'-'+version); (d/'DEBIAN').mkdir(parents=True)
                (d/'DEBIAN/control').write_text('Package: '+name+'\nVersion: '+version+'\nArchitecture: all\nMaintainer: QA <qa@example.invalid>\nDescription: disposable software client test\n'+('Depends: '+dependency+'\n' if dependency else ''))
                content=d/'usr/share'/name;content.mkdir(parents=True);(content/'version').write_text(version)
                out=root/(name+'_'+version+'_all.deb');run(['dpkg-deb','--build',str(d),str(out)]);return str(out)
            spec=root/(name+'.spec')
            spec.write_text('Name: '+name+'\nVersion: '+version+'\nRelease: 1\nSummary: software QA\nLicense: MIT\nBuildArch: noarch\n'+('Requires: '+dependency+'\n' if dependency else '')+'%description\nDisposable QA\n%install\nmkdir -p %{buildroot}/usr/share/%{name}\necho %{version} > %{buildroot}/usr/share/%{name}/version\n%files\n/usr/share/%{name}/version\n')
            run(['rpmbuild','--define','_topdir '+str(root/'build'),'-bb',str(spec)])
            return str(root/'build/RPMS/noarch'/(name+'-'+version+'-1.noarch.rpm'))
        lib=package('ventoy-qa-lib');app=package('ventoy-qa-app','ventoy-qa-lib');broken=package('ventoy-qa-broken','ventoy-qa-missing')
        kernel=package('kernel-ventoy-qa')
        with unittest.TestCase().assertRaises(RuntimeError):client.metadata(kernel,family,arch)
        baseline=client.installed(family)
        pkgs=[client.metadata(f,family,arch) for f in (app,lib)]
        assert client.transaction(pkgs,family,str(root/'transaction'),False)==2
        assert client.transaction(pkgs,family,str(root/'repeat'),False)==0
        newer=client.metadata(package('ventoy-qa-app','ventoy-qa-lib','1.10'),family,arch)
        middle=client.metadata(package('ventoy-qa-app','ventoy-qa-lib','1.9'),family,arch)
        chosen=client.select_latest([pkgs[0],newer,middle],family,arch)
        assert len(chosen)==1 and chosen[0]['version']==newer['version'],'must use native version ordering'
        assert client.transaction(chosen,family,str(root/'upgrade'),False)==1
        assert client.transaction(pkgs,family,str(root/'no-downgrade'),False)==0
        bad=[client.metadata(broken,family,arch)]
        before=client.installed(family)
        with unittest.TestCase().assertRaises(RuntimeError):client.transaction(bad,family,str(root/'broken'),False)
        assert client.installed(family)==before,'dependency failure changed installed packages'
        if family=='rpm':
            # Exercise yum/dnf's real dependency fallback against a private file
            # repository, without relying on a public mirror for test packages.
            repo=root/'repo';repo.mkdir()
            shutil.copyfile(package('ventoy-qa-repo-lib'),repo/'library.rpm')
            dependent=client.metadata(package('ventoy-qa-repo-app','ventoy-qa-repo-lib'),family,arch)
            run(['createrepo_c',str(repo)])
            repo_dir=Path('/etc/yum.repos.d');repo_dir.mkdir(exist_ok=True)
            moved=[];config=repo_dir/'ventoy-software-qa.repo'
            assert not config.exists()
            try:
                for original in repo_dir.glob('*.repo'):
                    backup=original.with_suffix('.repo.qa-disabled')
                    assert not backup.exists()
                    original.rename(backup);moved.append((original,backup))
                config.write_text('[ventoy-software-qa]\nname=Disposable software QA\nbaseurl=file://'+str(repo)+'\nenabled=1\ngpgcheck=0\n')
                assert client.transaction([dependent],family,str(root/'repo-transaction'),True)==1
                client.verify([dependent],family,baseline)
                assert 'ventoy-qa-repo-lib:noarch' in client.installed(family)
            finally:
                if config.exists():config.unlink()
                for original,backup in moved:backup.rename(original)
        client.verify(pkgs,family,baseline)
        print('NATIVE_SOFTWARE_PASS',family,'dependency transaction, idempotence, native version selection, upgrade, no downgrade, missing dependency, kernel rejection')


if __name__=='__main__':
    if '--native' in sys.argv:
        native()
    else:
        unittest.main()
