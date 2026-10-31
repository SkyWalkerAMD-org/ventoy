"""Exercise lifecycle failure/rollback and retention in disposable directories."""
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

if sys.platform != 'linux':
    print('SKIP: Linux deployment lifecycle tests')
    sys.exit(0)
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'deploy'))
import release
import maintenance


class Lifecycle(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory(prefix='vc-release-test-')
        self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name).resolve()
        self.iv=self.root/'iventoy';self.data=self.root/'data';self.base=self.root/'releases'
        for p in [self.iv/'iso',self.iv/'user/scripts/menu',self.data,self.root/'tools']:
            p.mkdir(parents=True)
        self.binary=self.root/'ventoy-center';self.binary.write_bytes(b'old binary')
        (self.root/'admin').write_text('fixture-only')
        self.setup=self.root/'tools/ventoy-linux-setup.run';self.setup.write_bytes(b'old setup')
        self.script=self.iv/'user/scripts/menu/user-data';self.script.write_bytes(b'old script')
        (self.iv/'iso/test.iso').write_bytes(b'fake ISO')
        self.cfg=self.root/'config.json'
        self.cfg.write_text(json.dumps(dict(iventoy_root=str(self.iv),data_dir=str(self.data),admin_token_file=str(self.root/'admin'),software_dir=str(self.root/'tools'))))
        self.entry=dict(id='entry',iso='test.iso',profile='ubuntu-22.04',preparation=dict(root=str(self.iv),script_path='user/scripts/menu/user-data',files={'user-data':release.digest(self.script)}))
        release.save(self.data/'state.json',dict(jobs=[],pxe_entries=[self.entry]))
        self.r=release.Release(SimpleNamespace(config=str(self.cfg),binary=str(self.binary),releases=str(self.base),name='alpha49',apply=True))
        self.r.dir.mkdir();(self.r.dir/'.release-owned').write_text('owned')
        for name,body in [('ventoy-center',b'new binary'),('ventoy-linux-setup.run',b'new setup'),('stage/iventoy/user/scripts/menu/user-data',b'new script')]:
            p=self.r.dir/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(body)
        (self.r.dir/'stage/iventoy/iso').mkdir(parents=True)
        os.link(self.iv/'iso/test.iso',self.r.dir/'stage/iventoy/iso/test.iso')
        entry=json.loads(json.dumps(self.entry));entry['preparation']['files']['user-data']=release.digest(self.r.dir/'stage/iventoy/user/scripts/menu/user-data')
        entry['preparation']['release']='new'
        self.manifest=dict(schema=1,name='alpha49',version='new',previous_version='old',source_commit='fixture',entry_count=1,config_sha256=release.digest(self.cfg),entries=[entry],files={n:release.digest(self.r.dir/n) for n in ['ventoy-center','ventoy-linux-setup.run','stage/iventoy/user/scripts/menu/user-data']})
        release.save(self.r.dir/'manifest.json',self.manifest)
        self.calls=[]
        self.patch=mock.patch.object(release.subprocess,'run',side_effect=lambda args,**kwargs:self.calls.append(args))
        self.patch.start();self.addCleanup(self.patch.stop)
        self.check=mock.patch.object(release.subprocess,'check_output',return_value='')
        self.check.start();self.addCleanup(self.check.stop)
        self.health=mock.patch.object(self.r,'healthy');self.health.start();self.addCleanup(self.health.stop)

    def test_publish_and_rollback_preserve_jobs_and_modes(self):
        self.binary.chmod(0o755)
        self.r.activate()
        self.assertEqual(self.binary.read_bytes(),b'new binary')
        state=release.read(self.r.state);state['jobs']=[dict(id='new-observation',state='installed')];release.save(self.r.state,state)
        self.r.rollback()
        self.assertEqual(self.binary.read_bytes(),b'old binary')
        self.assertEqual(self.setup.read_bytes(),b'old setup')
        self.assertEqual(self.script.read_bytes(),b'old script')
        self.assertEqual(self.binary.stat().st_mode&0o777,0o755)
        self.assertEqual(release.read(self.r.state)['jobs'],state['jobs'])
        self.assertEqual((self.iv/'iso/test.iso').read_bytes(),b'fake ISO')

    def test_api_uses_production_bind_and_loopback_staging(self):
        self.r.cfg['public_url']='https://192.168.1.2:26080'
        response=mock.Mock(status=200)
        response.read.return_value=b'{"status":"ok"}'
        with mock.patch.object(release.ssl,'create_default_context') as tls, mock.patch.object(release.socket,'create_connection') as dial, mock.patch.object(release.http.client.HTTPSConnection,'request',autospec=True,side_effect=lambda conn,*a,**kw:conn.connect()), mock.patch.object(release.http.client.HTTPSConnection,'getresponse',return_value=response):
            self.r.api('/healthz')
            self.assertEqual(dial.call_args[0][0],('192.168.1.2',26080))
            self.r.api('/healthz',port=26190)
            self.assertEqual(dial.call_args[0][0],('127.0.0.1',26190))
            self.assertEqual(tls.return_value.wrap_socket.call_args[1]['server_hostname'],'192.168.1.2')

    def test_failed_health_rolls_back(self):
        self.r.healthy.side_effect=[RuntimeError('candidate unhealthy'),None]
        with self.assertRaisesRegex(RuntimeError,'unhealthy'):self.r.activate()
        self.assertEqual(self.binary.read_bytes(),b'old binary')
        self.assertEqual(release.read(self.r.dir/'transaction.json')['state'],'rolled-back')

    def test_active_task_blocks_before_stop(self):
        state=release.read(self.r.state);state['jobs']=[dict(id='running',state='installing')];release.save(self.r.state,state)
        with self.assertRaisesRegex(RuntimeError,'Active tasks'):self.r.activate()
        self.assertFalse(any('stop' in c for c in self.calls))
        self.assertEqual(self.binary.read_bytes(),b'old binary')

    def test_tampered_release_and_configuration_fail_before_stop(self):
        self.cfg.write_text(self.cfg.read_text()+' ')
        with self.assertRaisesRegex(ValueError,'configuration changed'):self.r.activate()
        self.assertFalse(any('stop' in c for c in self.calls))
        (self.r.dir/'ventoy-center').write_bytes(b'tampered')
        with self.assertRaisesRegex(ValueError,'Release file changed'):self.r.verify()

    def test_backup_tamper_and_foreign_release_rollback_rejected(self):
        self.r.activate();self.calls.clear()
        (self.r.dir/'backup/0').write_bytes(b'tampered')
        with self.assertRaisesRegex(ValueError,'integrity'):self.r.rollback()
        self.assertFalse(any('stop' in c for c in self.calls))
        release.save(self.data/'release-status.json',dict(name='another'))
        with self.assertRaisesRegex(ValueError,'currently published'):self.r.rollback()

    def test_retention_mounts_symlinks_and_last_iso_link(self):
        for i in range(4):
            p=self.base/('old'+str(i));p.mkdir();(p/'.release-owned').touch();release.save(p/'manifest.json',dict(name=p.name,schema=1,version='old'))
            os.utime(p/'manifest.json',(100+i,100+i))
        rows,unknown=maintenance.inventory(self.base,'old0',[],now=2000000)
        candidates={r['name'] for r in rows if r['eligible']}
        self.assertEqual(candidates,{'old1','old2'})
        p=self.base/'old1';(p/'lonely.iso').write_bytes(b'only copy')
        self.assertFalse(maintenance.safe_tree(p,[])[0])
        p=self.base/'old2';(p/'outside').symlink_to(self.root)
        self.assertFalse(maintenance.safe_tree(p,[])[0])
        self.assertFalse(maintenance.safe_tree(self.r.dir,[self.r.dir/'stage'])[0])
        (self.base/'foreign').mkdir()
        self.assertIn('foreign',maintenance.inventory(self.base,'old0',[],now=2000000)[1])


if __name__=='__main__':unittest.main()
