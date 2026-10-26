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
    manual.prepare(path, release, packages, kernel['lock_installer_kernel'])
    before = path.read_bytes()
    config = yaml.safe_load(before)
    assert set(config) == {'version', 'interactive-sections', 'refresh-installer', 'apt', 'kernel'}
    assert {'storage', 'identity', 'network', 'apt', 'source', 'ssh'} <= set(config['interactive-sections'])
    assert not {'*', 'refresh-installer'} & set(config['interactive-sections'])
    assert config['kernel'] == {'package': 'linux-image-' + release}
    assert config['refresh-installer'] == {'update': False}
    assert 'Package-Blacklist { ".*"; }' in config['apt']['conf']
    assert config['apt']['preferences'][-1]['pin-priority'] == -1
    manual.prepare(path, release, packages, kernel['lock_installer_kernel'])
    assert path.read_bytes() == before
    for kind in ('external', 'changed', 'symlink', 'missing-kernel'):
        target = root / kind
        target.mkdir()
        answer = target / 'autoinstall.yaml'
        if kind == 'external': answer.write_text('version: 1\nstorage: {layout: {name: lvm}}\n')
        if kind == 'changed':
            manual.prepare(answer, release, packages, kernel['lock_installer_kernel'])
            answer.write_text(answer.read_text() + '\n# user changed\n')
        if kind == 'symlink': answer.symlink_to(path)
        original = answer.read_bytes() if answer.exists() else None
        try:
            manual.prepare(answer, release, '' if kind == 'missing-kernel' else packages, kernel['lock_installer_kernel'])
        except (ValueError, RuntimeError): pass
        else: raise AssertionError('Accepted unsafe policy input: ' + kind)
        assert (answer.read_bytes() if answer.exists() else None) == original
print('PASS: all manual screens remain interactive, exact kernel policy, idempotency and external answer preservation')
