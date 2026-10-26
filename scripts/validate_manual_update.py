"""Exercise policy-only interactive defaults, including refusal before any disk work."""
import importlib.util
from pathlib import Path
import tempfile
import yaml

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT / 'internal/center/assets/manual-preload'
spec = importlib.util.spec_from_file_location('manual_policy', ASSETS / 'manual-ubuntu-policy.py')
manual = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manual)
kernel = {'__name__': 'policy_test'}
source = (ROOT / 'internal/center/templates/ubuntu-kernel.py').read_text()
exec(compile(source.replace('{{.KernelPackages}}', 'linux-image* linux-modules* linux-headers* linux-generic*'), 'kernel.py', 'exec'), kernel)
release = '5.15.0-78-generic'
packages = 'linux-image-' + release + '\t5.15.0-78.85\tinstalled\n'
packages += 'linux-modules-' + release + '\t5.15.0-78.85\tinstalled\n'
with tempfile.TemporaryDirectory() as tmp:
    root = Path(tmp)
    path = root / 'autoinstall.yaml'
    manual.prepare(path, release, packages, kernel['lock_installer_kernel'], '22.04')
    before = path.read_bytes()
    config = yaml.safe_load(before)
    assert set(config) == {'version', 'interactive-sections', 'refresh-installer', 'apt', 'kernel'}
    assert {'storage', 'identity', 'network', 'apt', 'source', 'ssh'} <= set(config['interactive-sections'])
    assert not {'*', 'refresh-installer'} & set(config['interactive-sections'])
    assert config['kernel'] == {'package': 'linux-image-' + release}
    assert config['refresh-installer'] == {'update': False}
    assert 'Package-Blacklist { ".*"; }' in config['apt']['conf']
    assert config['apt']['preferences'][-1]['pin-priority'] == -1
    assert config['apt']['primary'][0]['uri'] == 'https://archive.ubuntu.com/ubuntu'
    assert config['apt']['security'][0]['uri'] == 'https://security.ubuntu.com/ubuntu'
    assert config['apt']['preserve_sources_list'] is False
    manual.prepare(path, release, packages, kernel['lock_installer_kernel'], '22.04')
    assert path.read_bytes() == before
    for kind in ('external', 'changed', 'symlink', 'missing-kernel'):
        target = root / kind
        target.mkdir()
        answer = target / 'autoinstall.yaml'
        if kind == 'external': answer.write_text('version: 1\nstorage: {layout: {name: lvm}}\n')
        if kind == 'changed':
            manual.prepare(answer, release, packages, kernel['lock_installer_kernel'], '22.04')
            answer.write_text(answer.read_text() + '\n# user changed\n')
        if kind == 'symlink': answer.symlink_to(path)
        original = answer.read_bytes() if answer.exists() else None
        try:
            manual.prepare(answer, release, '' if kind == 'missing-kernel' else packages, kernel['lock_installer_kernel'], '22.04')
        except (ValueError, RuntimeError): pass
        else: raise AssertionError('Accepted unsafe policy input: ' + kind)
        assert (answer.read_bytes() if answer.exists() else None) == original
    for version in ('20.04', '22.04', '24.04', '24.10', '25.04', '26.04'):
        target = root / version
        target.mkdir()
        os_release = target / 'os-release'
        os_release.write_text('ID=ubuntu\nVERSION_ID="' + version + '"\n')
        assert manual.ubuntu_version(os_release) == version
        answer = target / 'autoinstall.yaml'
        manual.prepare(answer, release, packages, kernel['lock_installer_kernel'], version)
        apt = yaml.safe_load(answer.read_text())['apt']
        archived = version in ('24.10', '25.04')
        for section, live_host in (('primary', 'archive'), ('security', 'security')):
            assert apt[section] == [{'arches': ['default'], 'uri':
                                   'https://%s.ubuntu.com/ubuntu' % ('old-releases' if archived else live_host)}]
print('PASS: all manual screens remain interactive, exact kernel policy, idempotency and external answer preservation')
