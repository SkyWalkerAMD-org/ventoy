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
from http.server import BaseHTTPRequestHandler, HTTPServer

sys.dont_write_bytecode = True
source = Path(sys.argv.pop()) if len(sys.argv) > 1 and sys.argv[-1].endswith('.py') else Path(__file__).resolve().parents[1] / 'internal/center/assets/manual/software.py'
spec = importlib.util.spec_from_file_location('software', source)
client = importlib.util.module_from_spec(spec)
spec.loader.exec_module(client)


class Boundaries(unittest.TestCase):
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
