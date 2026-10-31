#!/usr/bin/env python3
"""Report disk usage and retire old managed releases. Default is read-only.

Keep the current release and at least two newest releases, and retain every
release for seven days. Unknown directories, mounted trees, symlinks and the
last link to an ISO are never removed. This does not clean original ISOs.
"""
import argparse
import datetime
import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
from release import read, real, child, save


def tree_usage(folder):
    size, allocated, files, seen = 0, 0, 0, set()
    for base, dirs, names in os.walk(str(folder), followlinks=False):
        dirs[:] = [d for d in dirs if not (Path(base)/d).is_symlink()]
        for name in names:
            p = Path(base)/name
            if p.is_symlink() or not p.is_file():
                continue
            st = p.stat()
            size += st.st_size
            files += 1
            inode = (st.st_dev, st.st_ino)
            if inode not in seen:
                allocated += st.st_blocks*512
                seen.add(inode)
    return dict(logical_bytes=size, allocated_bytes=allocated, files=files)


def safe_tree(folder, mounts):
    if any(m == folder or folder in m.parents for m in mounts):
        return False, 'mounted tree'
    device = folder.stat().st_dev
    for base, dirs, names in os.walk(str(folder), followlinks=False):
        for name in dirs+names:
            p = Path(base)/name
            if p.is_symlink():
                return False, 'symlink in release'
            st = p.stat()
            if st.st_dev != device:
                return False, 'different filesystem'
            if p.suffix.lower() == '.iso' and st.st_nlink < 2:
                return False, 'last ISO link'
            if not p.is_dir() and not p.is_file():
                return False, 'special file'
    return True, ''


def inventory(base, current, mounts, now=None):
    now = time.time() if now is None else now
    releases, unknown = [], []
    for p in base.iterdir():
        if p.name == 'release.lock':
            continue
        if p.is_symlink() or not p.is_dir() or any(x.is_symlink() or not x.is_file() for x in (p/'.release-owned', p/'manifest.json')):
            unknown.append(p.name)
            continue
        real(p)
        m = read(p/'manifest.json')
        if m.get('name') != p.name or m.get('schema') != 1:
            unknown.append(p.name)
            continue
        releases.append((p, (p/'manifest.json').stat().st_mtime, m))
    releases.sort(key=lambda x: x[1], reverse=True)
    keep = {p.name for p, _, _ in releases[:2]} | {current}
    rows = []
    for p, modified, m in releases:
        safe, reason = safe_tree(p, mounts)
        if p.name in keep:
            reason = 'current or two newest releases'
        elif now-modified < 7*86400:
            reason = 'younger than seven days'
        eligible = safe and not reason
        rows.append(dict(name=p.name, version=m.get('version'), modified=modified,
                         eligible=eligible, reason=reason, **tree_usage(p)))
    return rows, unknown


def idle(config):
    jobs = read(Path(config['data_dir'])/'state.json').get('jobs', [])
    if any(j['state'] not in ('installed', 'failed', 'canceled', 'enrolled') for j in jobs):
        raise RuntimeError('Active installation task; cleanup deferred')
    connections = subprocess.check_output(['ss', '-Htn', 'state', 'established',
        '( sport = :3260 or sport = :10809 or sport = :12049 or sport = :10445 or sport = :16000 )'], universal_newlines=True)
    if connections.strip():
        raise RuntimeError('Network media clients still connected; cleanup deferred')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', default='/var/lib/ventoy-center/config.json')
    parser.add_argument('--releases', required=True)
    parser.add_argument('--apply', action='store_true', help='Remove eligible managed releases only')
    parser.add_argument('--report', action='store_true', help='Save public storage report for the management page')
    args = parser.parse_args()
    config = read(real(args.config))
    base = real(args.releases)
    if base == Path('/') or not base.is_dir():
        raise ValueError('Existing managed release directory required')
    data = real(config['data_dir'])
    public = data/'release-status.json'
    current = read(public).get('name', '') if public.exists() else ''
    mounts = [Path(x.split()[4].replace('\\040', ' ')) for x in Path('/proc/self/mountinfo').read_text().splitlines()]
    with (base/'release.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        rows, unknown = inventory(base, current, mounts)
        removed = []
        if args.apply:
            idle(config)
            for item in rows:
                if item['eligible']:
                    target = child(base, item['name'])
                    safe, reason = safe_tree(target, mounts)
                    if not safe:
                        raise RuntimeError('Cleanup recheck failed: '+reason)
                    shutil.rmtree(str(target))
                    removed.append(item['name'])
            rows, unknown = inventory(base, current, mounts)
        volumes = []
        for path in dict.fromkeys(['/', config['iventoy_root'], str(base)]):
            usage = shutil.disk_usage(path)
            percent = round(100.0*usage.used/usage.total, 1)
            volumes.append(dict(path=path, total=usage.total, used=usage.used, free=usage.free,
                                percent=percent, level='critical' if percent >= 95 else 'warning' if percent >= 85 else 'normal'))
        report = dict(time=datetime.datetime.utcnow().isoformat()+'Z', volumes=volumes,
                      releases=rows, unknown=unknown, removed=removed,
                      policy='keep current + newest two; minimum seven days; unknown paths retained')
        if args.report:
            save(data/'storage-status.json', report)
            st = (data/'state.json').stat()
            os.chown(data/'storage-status.json', st.st_uid, st.st_gid)
        print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
