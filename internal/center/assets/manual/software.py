#!/usr/bin/env python
"""Install the controller's matching default group and resolve missing dependencies.

Python 2.7.9+ / 3.x, using the installed package manager. All files are downloaded
and checked before a transaction. Distribution compatibility is declared by the
administrator's directory placement; internal package names/versions/architectures
and dependencies are checked locally. Package maintainer scripts are trusted code.
"""
from __future__ import print_function
import fnmatch
import hashlib
import json
import os
import re
import shlex
import shutil
import ssl
import subprocess
import sys
import tempfile
import time
try:
    from urllib.request import Request, build_opener, HTTPSHandler, HTTPRedirectHandler
    from urllib.parse import urlencode, urlsplit
except ImportError:
    from urllib2 import Request, build_opener, HTTPSHandler, HTTPRedirectHandler
    from urllib import urlencode
    from urlparse import urlsplit

HERE = os.path.dirname(os.path.realpath(__file__))
STATE = '/var/lib/ventoy-center/software'
RPM_IDS = ('centos', 'rocky', 'almalinux', 'rhel', 'ol', 'scientific', 'anolis')
HASH = re.compile(r'^[0-9a-f]{64}$')
NAME = re.compile(r'^[a-zA-Z0-9][a-zA-Z0-9.+_-]*$')
FILENAME = re.compile(r'^[a-zA-Z0-9][a-zA-Z0-9._+~-]{0,240}\.(deb|rpm)$')
MAX_FILE = 4 * 1024**3
MAX_TOTAL = 8 * 1024**3


def run(args, echo=False):
    p = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                         env=dict(os.environ, LC_ALL='C', DEBIAN_FRONTEND='noninteractive'))
    if echo:
        chunks = []
        for line in iter(p.stdout.readline, b''):
            chunks.append(line)
            sys.stdout.write(line.decode('utf-8', 'replace'))
            sys.stdout.flush()
        p.wait()
        data = b''.join(chunks).decode('utf-8', 'replace')
    else:
        data = p.communicate()[0].decode('utf-8', 'replace')
    if p.returncode:
        raise RuntimeError('%s failed (exit %s):\n%s' % (args[0], p.returncode, data[-12000:]))
    return data.strip()


def target():
    values = {}
    with open('/etc/os-release') as f:
        for line in f:
            if '=' in line and not line.lstrip().startswith('#'):
                k, v = line.strip().split('=', 1)
                words = shlex.split(v)
                if len(words) == 1:
                    values[k] = words[0]
    distro, version = values.get('ID'), values.get('VERSION_ID', '')
    if distro in ('ubuntu', 'debian'):
        arch = run(['dpkg', '--print-architecture'])
    elif distro in RPM_IDS:
        arch = run(['rpm', '--eval', '%{_arch}'])
    else:
        raise RuntimeError('Unsupported distribution: %s' % distro)
    if not re.match(r'^[0-9]+(\.[0-9]+){0,2}$', version):
        raise RuntimeError('Invalid distribution version')
    return dict(os=distro, version=version, arch=arch)


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise RuntimeError('Software server redirect refused')


class Client(object):
    def __init__(self, config):
        self.origin = config['url'].rstrip('/')
        u = urlsplit(self.origin)
        if (u.username or u.password or u.path or u.query or u.fragment or
                not (u.scheme == 'https' or (u.scheme == 'http' and u.hostname in ('127.0.0.1', '::1')))):
            raise RuntimeError('Software server must be an HTTPS origin')
        self.token = config['token']
        if not HASH.match(self.token):
            raise RuntimeError('Invalid software download credential')
        ca = config.get('ca_file')
        if ca and ca != 'software-ca.crt':
            raise RuntimeError('Invalid software CA path')
        context = ssl.create_default_context(cafile=os.path.join(HERE, ca) if ca else None)
        self.opener = build_opener(NoRedirect(), HTTPSHandler(context=context))

    def get(self, endpoint, params):
        request = Request(self.origin + '/software/v1/' + endpoint + '?' + urlencode(params),
                          headers={'Authorization': 'Bearer ' + self.token})
        return self.opener.open(request, timeout=60)

    def manifest(self, wanted):
        response = self.get('manifest', wanted)
        try:
            data = response.read(1024*1024 + 1)
        finally:
            response.close()
        if len(data) > 1024*1024:
            raise RuntimeError('Manifest is too large')
        manifest = json.loads(data.decode('utf-8'))
        validate_manifest(manifest, wanted)
        return manifest

    def download(self, item, dest):
        digest, size = hashlib.sha256(), 0
        response = self.get('package', {'path': item['path'], 'sha256': item['sha256']})
        try:
            with open(dest, 'wb') as f:
                while True:
                    block = response.read(1024*1024)
                    if not block:
                        break
                    size += len(block)
                    if size > item['size']:
                        raise RuntimeError('Package size changed: ' + item['path'])
                    digest.update(block)
                    f.write(block)
        finally:
            response.close()
        if size != item['size'] or digest.hexdigest() != item['sha256']:
            raise RuntimeError('Package checksum mismatch: ' + item['path'])


def validate_manifest(manifest, wanted):
    if manifest.get('schema') != 1 or manifest.get('target') != wanted:
        raise RuntimeError('Software manifest target/schema mismatch')
    group = manifest.get('group')
    if group is None:
        return
    versions = [wanted['version']]
    if wanted['os'] != 'ubuntu':
        versions.append(wanted['version'].split('.')[0])
    if (group.get('os') != wanted['os'] or group.get('arch') != wanted['arch'] or
            group.get('version') not in versions or group.get('errors')):
        raise RuntimeError('Software group is not compatible with this host')
    prefix = '/'.join([group['os'], group['version'], group['arch'], 'default'])
    flat = group.get('id') == 'auto/' + prefix
    if group.get('id') != prefix and not flat:
        raise RuntimeError('Invalid software group identifier')
    files = group.get('files', [])
    if not isinstance(files, list) or not 0 < len(files) <= 256:
        raise RuntimeError('Default group must contain 1..256 packages')
    seen, total = set(), 0
    ext = '.deb' if wanted['os'] in ('ubuntu', 'debian') else '.rpm'
    for item in files:
        path = item.get('path', '')
        name = path.rsplit('/', 1)[-1]
        if (path != (name if flat else prefix + '/' + name) or not FILENAME.match(name) or not path.endswith(ext) or
                path in seen or not HASH.match(item.get('sha256', '')) or
                not isinstance(item.get('size'), (int, type(MAX_FILE))) or not 0 < item['size'] <= MAX_FILE):
            raise RuntimeError('Invalid package in manifest')
        seen.add(path)
        total += item['size']
    if total > MAX_TOTAL:
        raise RuntimeError('Default group exceeds 8 GiB')


def protected(name):
    patterns = ('kernel* linux-image* linux-headers* linux-modules* linux-generic* linux-virtual* '
                'linux-lowlatency* linux-oem* linux-aws* linux-azure* linux-gcp* linux-gke* '
                'linux-oracle* linux-kvm* linux-raspi* linux-riscv*').split()
    return any(fnmatch.fnmatchcase(name, p) for p in patterns)


def metadata(file, family, arch):
    if family == 'deb':
        result = run(['dpkg-deb', '-f', file, 'Package', 'Version', 'Architecture', 'Provides'])
        fields = {}
        for line in result.splitlines():
            if ': ' in line:
                k, v = line.split(': ', 1)
                fields[k.lower()] = v
        name, version, package_arch = fields.get('package', ''), fields.get('version', ''), fields.get('architecture', '')
        provides = fields.get('provides', '')
    else:
        result = run(['rpm', '-qp', '--qf', '%{NAME}\n%{EPOCHNUM}:%{VERSION}-%{RELEASE}\n%{ARCH}\n', file])
        # rpm can emit signature warnings before its query output.
        name, version, package_arch = result.splitlines()[-3:]
        provides = run(['rpm', '-qp', '--provides', file])
        obsoletes = run(['rpm', '-qp', '--obsoletes', file])
        installed_names = set(k.split(':')[0] for k in installed('rpm'))
        for obsolete in obsoletes.splitlines():
            replaced = obsolete.split()[0] if obsolete.split() else ''
            if replaced in installed_names or protected(replaced):
                raise RuntimeError('RPM would obsolete an installed/protected package: ' + replaced)
    if not NAME.match(name) or not version or package_arch not in (arch, 'all' if family == 'deb' else 'noarch'):
        raise RuntimeError('Invalid package metadata or architecture: ' + os.path.basename(file))
    if protected(name) or any(protected(x) for x in re.findall(r'[a-zA-Z0-9][a-zA-Z0-9.+_-]*', provides)):
        raise RuntimeError('Kernel packages cannot be distributed through software setup: ' + name)
    return dict(name=name, version=version, arch=package_arch, target_arch=arch, file=file)


def installed(family):
    result = {}
    if family == 'deb':
        data = run(['dpkg-query', '-W', '-f=${Package}\t${Architecture}\t${Version}\t${db:Status-Status}\n'])
        for line in data.splitlines():
            parts = line.split('\t')
            if len(parts) == 4 and parts[3] == 'installed':
                result[parts[0] + ':' + parts[1]] = parts[2]
    else:
        data = run(['rpm', '-qa', '--qf', '%{NAME}\t%{ARCH}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}\n'])
        # Keep all installed kernel versions, including installonly packages.
        for line in data.splitlines():
            parts = line.split('\t')
            if len(parts) == 3:
                key = parts[0] + ':' + parts[1]
                result.setdefault(key, []).append(parts[2])
        result = dict((k, '|'.join(sorted(v))) for k, v in result.items())
    return result


def compare_versions(a, b, family):
    if not all(re.match(r'^[A-Za-z0-9:._+~^-]+$', v) for v in (a,b)):
        raise RuntimeError('Invalid package version')
    if a == b:
        return 0
    if family == 'deb':
        for op, value in (('eq', 0), ('gt', 1), ('lt', -1)):
            rc = subprocess.call(['dpkg', '--compare-versions', a, op, b])
            if rc == 0:
                return value
            if rc != 1:
                raise RuntimeError('Cannot compare Debian package versions')
    else:
        def evr(v):
            epoch, rest = v.split(':',1)
            version, release = rest.rsplit('-',1)
            return epoch, version, release
        ea, va, ra = evr(a); eb, vb, rb = evr(b)
        if int(ea) != int(eb):
            return 1 if int(ea) > int(eb) else -1
        try:
            import rpm
            return rpm.labelCompare((ea,va,ra), (eb,vb,rb))
        except ImportError:
            # Native RPM Lua comparator preserves epochs, ~ prereleases, etc.;
            # never approximate package ordering with strings or filenames.
            for x,y in ((va,vb),(ra,rb)):
                result = int(run(['rpm','--eval','%{lua:print(rpm.vercmp("'+x+'", "'+y+'"))}']))
                if result:
                    return 1 if result > 0 else -1
            return 0
    raise RuntimeError('Cannot compare package versions')


def current_version(current, package, family):
    # A noarch/all package may have replaced a native-architecture build.
    arches = (package['arch'],package.get('target_arch',package['arch']),'all' if family=='deb' else 'noarch')
    versions = [v for k,values in current.items() if k.split(':')[0] == package['name'] and k.split(':')[1] in arches for v in values.split('|')]
    latest = None
    for version in versions:
        if latest is None or compare_versions(version,latest,family)>0:
            latest=version
    return latest


def select_latest(candidates, family, arch):
    selected = {}
    for p in candidates:
        old = selected.get(p['name'])
        if old is None:
            selected[p['name']] = p
            continue
        cmp = compare_versions(p['version'],old['version'],family)
        if cmp > 0 or (cmp == 0 and p['arch'] == arch and old['arch'] != arch):
            selected[p['name']] = p
        elif cmp == 0 and p['arch'] == old['arch'] and p.get('sha256') != old.get('sha256'):
            raise RuntimeError('Conflicting builds share the same package name/version/architecture: ' + p['name'])
    return [selected[name] for name in sorted(selected)]


def verify(packages, family, before=None):
    after = installed(family)
    for p in packages:
        current = current_version(after,p,family)
        if current is None or compare_versions(current,p['version'],family)<0:
            raise RuntimeError('Installed package verification failed: ' + p['name'])
    if before is not None:
        old_kernel = dict((k, v) for k, v in before.items() if protected(k.split(':')[0]))
        new_kernel = dict((k, v) for k, v in after.items() if protected(k.split(':')[0]))
        if new_kernel != old_kernel:
            raise RuntimeError('Kernel package set changed; inspect software transaction log')
    return after


def transaction(packages, family, work, allow_dependencies=True):
    before = installed(family)
    pending = [p for p in packages if current_version(before,p,family) is None or compare_versions(current_version(before,p,family),p['version'],family)<0]
    for p in packages:
        if p not in pending:
            print('SKIP: %s is already installed at the requested version or newer.' % p['name'])
    if pending:
        files = [p['file'] for p in pending]
        if family == 'deb':
            for folder in ('lists/partial', 'archives/partial', 'sourceparts'):
                os.makedirs(os.path.join(work, folder))
            sources = os.path.join(work, 'sources.list')
            with open(sources, 'w'):
                pass
            base = ['apt-get', '-o', 'Dir::Etc::sourcelist='+sources, '-o', 'Dir::Etc::sourceparts='+os.path.join(work,'sourceparts'),
                    '-o', 'Dir::State::lists=' + os.path.join(work, 'lists'),
                    '-o', 'Dir::Cache::archives=' + os.path.join(work, 'archives'),
                    '-o', 'APT::Get::List-Cleanup=0', '-o', 'Dpkg::Options::=--force-confold',
                    '--no-remove', '--no-install-recommends', '--no-install-suggests']
            try:
                plan = run(base + ['--simulate', 'install'] + files, echo=True)
            except RuntimeError:
                if not allow_dependencies:
                    raise
                print('Resolving missing dependencies from configured repositories (no upgrade command).')
                # Refresh indices only for this explicit invocation. Timers and
                # periodic-update settings remain disabled. No repository added.
                run(['apt-get', '-o', 'APT::Update::Error-Mode=any', '-o', 'Acquire::Retries=1', '-o', 'Acquire::http::Timeout=30', '-o', 'Acquire::https::Timeout=30', 'update'], echo=True)
                base = ['apt-get', '--no-remove', '--no-install-recommends', '--no-install-suggests',
                        '-o', 'Dpkg::Options::=--force-confold']
                plan = run(base + ['--simulate', 'install'] + files, echo=True)
            for line in plan.splitlines():
                if line.startswith('Remv ') or (line.startswith('Inst ') and protected(line.split()[1])):
                    raise RuntimeError('Package transaction would remove software or change the kernel')
            run(base + ['-y', 'install'] + files, echo=True)
        else:
            # First resolve against the supplied set and installed database.
            # Use configured repositories only if dependencies are missing.
            try:
                run(['rpm', '-U', '--test'] + files, echo=True)
            except RuntimeError:
                if not allow_dependencies:
                    raise
                manager = next((p for p in ('/usr/bin/dnf', '/usr/bin/yum') if os.path.isfile(p)), None)
                if not manager:
                    raise RuntimeError('Missing dependencies; yum/dnf is not available')
                # Only this install transaction and its dependencies, never a
                # whole-system update/upgrade/distro-sync. Preserve user locks.
                args = [manager, '-y', '--exclude=kernel*', '--setopt=obsoletes=0', '--setopt=install_weak_deps=False', '--setopt=timeout=30', '--setopt=retries=1']
                run(args + ['--setopt=tsflags=test', 'install'] + files, echo=True)
                run(args + ['install'] + files, echo=True)
            else:
                run(['rpm', '-Uvh'] + files, echo=True)
    verify(packages, family, before)
    return len(pending)


def save_report(data):
    if not os.path.isdir(STATE):
        os.makedirs(STATE, 0o700)
    fd, tmp = tempfile.mkstemp(prefix='.result-', dir=STATE)
    try:
        with os.fdopen(fd, 'w') as f:
            json.dump(data, f, indent=2, sort_keys=True)
            f.write('\n')
            f.flush()
            os.fsync(f.fileno())
        os.rename(tmp, os.path.join(STATE, 'last-result.json'))
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def execute(mode):
    wanted = target()
    family = 'deb' if wanted['os'] in ('ubuntu', 'debian') else 'rpm'
    if mode == '--check-software':
        with open(os.path.join(STATE, 'last-result.json')) as f:
            report = json.load(f)
        if report.get('status') != 'verified' or report.get('target') != wanted:
            raise RuntimeError('No successfully installed matching software group to verify')
        verify(report['packages'], family)
        print('VERIFIED: %s packages are at the requested version or newer.' % len(report['packages']))
        return
    with open(os.path.join(HERE, 'software-client.json')) as f:
        config = json.load(f)
    if not config.get('enabled'):
        print('SKIPPED: controller software library is not configured.')
        return
    client = Client(config)
    manifest = client.manifest(wanted)
    group = manifest['group']
    if group is None:
        print('SKIPPED: no default software group for %(os)s %(version)s %(arch)s.' % wanted)
        return
    print('Default software group: %s (%s packages)' % (group['id'], len(group['files'])))
    for f in group['files']:
        print('  %s  %s bytes  sha256=%s' % (f['path'].split('/')[-1], f['size'], f['sha256']))
    if mode == '--list-software':
        return
    # Immutable manifest hashes plus native installed versions allow repeat runs
    # to avoid retransferring already verified payloads. A replaced/new file
    # invalidates the shortcut and is inspected afresh before any transaction.
    previous = os.path.join(STATE, 'last-result.json')
    try:
        with open(previous) as f:
            last = json.load(f)
        if last.get('status') == 'verified' and last.get('manifest') == manifest:
            verify(last['packages'],family)
            if os.path.isdir('/var/lib/ventoy-center/kernel-policy'):
                run(['sh',os.path.join(HERE,'linux-update-policy.sh'),'--check'],echo=True)
            print('VERIFIED: unchanged software group; installed versions checked; no package download or installation needed.')
            return
    except (IOError, ValueError, RuntimeError):
        pass
    work = tempfile.mkdtemp(prefix='ventoy-software-', dir='/var/tmp')
    report = dict(status='failed', target=wanted, group=group['id'], manifest=manifest,
                  time=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), packages=[])
    try:
        stat = os.statvfs(work)
        if stat.f_bavail * stat.f_frsize < sum(f['size'] for f in group['files']) + 128*1024**2:
            raise RuntimeError('Insufficient free space to stage software packages')
        candidates = []
        for item in group['files']:
            dest = os.path.join(work, item['path'].split('/')[-1])
            print('Downloading and verifying: ' + item['path']);sys.stdout.flush()
            client.download(item, dest)
            p = metadata(dest, family, wanted['arch'])
            p['sha256'] = item['sha256']
            if family == 'rpm':
                tag = re.search(r'\.el([0-9]+)(?:[._-]|$)', p['version'])
                if tag and tag.group(1) != wanted['version'].split('.')[0]:
                    floor = group.get('rpm_min_el', {}).get(p['name'])
                    if not isinstance(floor, int) or int(tag.group(1)) < floor or int(wanted['version'].split('.')[0]) < int(tag.group(1)):
                        raise RuntimeError('RPM Enterprise Linux release does not match this host: ' + p['name'])
            candidates.append(p)
        report['packages'] = select_latest(candidates, family, wanted['arch'])
        changed = transaction(report['packages'], family, work)
        policy = os.path.join(HERE, 'linux-update-policy.sh')
        if os.path.isdir('/var/lib/ventoy-center/kernel-policy'):
            run(['sh', policy, '--check'], echo=True)
        report['status'], report['changed'] = 'verified', changed
        print('VERIFIED: %s packages installed; %s changed; kernel package set preserved.' % (len(report['packages']), changed))
    except Exception as e:
        report['error'] = str(e)
        raise
    finally:
        for p in report['packages']:
            p.pop('file', None)
        save_report(report)
        shutil.rmtree(work)


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in ('--software', '--list-software', '--check-software'):
        raise RuntimeError('Use setup --software, --list-software or --check-software')
    if os.geteuid() != 0:
        raise RuntimeError('Run as root or sudo')
    os.umask(0o077)
    import fcntl
    if not os.path.isdir(STATE):
        os.makedirs(STATE, 0o700)
    with open(os.path.join(STATE, 'lock'), 'a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        execute(sys.argv[1])


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        print('Software setup FAILED: %s' % exc, file=sys.stderr)
        sys.exit(1)
