#!/usr/bin/env python3
"""Rebuild the offline script and source-inclusive tarball using Go and Python stdlib."""
from pathlib import Path
import argparse
import base64
import gzip
import hashlib
import json
import os
import re
import struct
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parent


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--go', default='go')
    parser.add_argument('--bash', default='bash')
    parser.add_argument('--output', type=Path, default=ROOT.parent)
    args = parser.parse_args()
    version = re.search(r'const version = "([0-9.]+)"', (ROOT/'src/main.go').read_text()).group(1)
    env = dict(os.environ, GOTOOLCHAIN='local', CGO_ENABLED='0')
    env.pop('GOOS', None)
    env.pop('GOARCH', None)
    subprocess.run([args.go, 'test', './...'], cwd=ROOT/'src', env=env, check=True)
    env.update(GOOS='linux', GOARCH='amd64', GOAMD64='v1')
    subprocess.run([args.go, 'vet', './...'], cwd=ROOT/'src', env=env, check=True)
    subprocess.run([args.go, 'build', '-trimpath', '-buildvcs=false', '-ldflags=-s -w', '-o', str(ROOT/'p95-priority'), '.'],
                   cwd=ROOT/'src', env=env, check=True)
    for name in ('install.sh', 'setup-header.sh'):
        subprocess.run([args.bash, '-n', str(ROOT/name)], check=True)
    binary = (ROOT/'p95-priority').read_bytes()
    header = struct.unpack_from('<16sHHIQQQIHHHHHH', binary)
    assert binary[:6] == b'\x7fELF\x02\x01' and header[2] == 62
    assert 3 not in [struct.unpack_from('<I', binary, header[5]+i*header[9])[0] for i in range(header[10])]
    report = dict(version=version, target='linux/amd64', static=True, cgo_enabled=False,
                  validation={'go_tests':'passed', 'linux_vet':'passed', 'linux_cross_build':'passed',
                              'bash_syntax':'passed', 'hardware_stress':'not run; verify on target after installation'},
                  scope='Independent runtime helper; Linux selfcheck executes at installation time.')
    (ROOT/'BUILD-VERIFICATION.json').write_text(json.dumps(report,indent=2)+'\n', encoding='utf-8', newline='\n')
    included = [p for p in sorted(ROOT.rglob('*')) if p.is_file() and p.name != 'SHA256SUMS' and '__pycache__' not in p.parts]
    manifest = ''.join(sha(p)+'  '+p.relative_to(ROOT).as_posix()+'\n' for p in included)
    (ROOT/'SHA256SUMS').write_text(manifest, encoding='ascii', newline='\n')
    args.output.mkdir(parents=True, exist_ok=True)
    archive = args.output/('p95-priority-helper-%s-linux-amd64.tar.gz'%version)
    with archive.open('wb') as raw:
        with gzip.GzipFile(filename='',mode='wb',fileobj=raw,mtime=0) as gz:
            with tarfile.open(fileobj=gz,mode='w') as tf:
                for path in [ROOT]+[p for p in sorted(ROOT.rglob('*')) if '__pycache__' not in p.parts]:
                    member='p95-priority-helper'+('/'+path.relative_to(ROOT).as_posix() if path!=ROOT else '')
                    info=tf.gettarinfo(str(path),arcname=member)
                    assert info.isfile() or info.isdir()
                    info.uid=info.gid=info.mtime=0
                    info.uname=info.gname='root'
                    info.mode=0o755 if path.is_dir() or path.name in ('p95-priority','install.sh','build-release.py') else 0o644
                    if path.is_file():
                        with path.open('rb') as src: tf.addfile(info,src)
                    else: tf.addfile(info)
    text=(ROOT/'setup-header.sh').read_text(encoding='utf-8')
    for key,value in {'VERSION':version,'ARCHIVE_SHA':sha(archive),'MANIFEST_SHA':sha(ROOT/'SHA256SUMS'),
                      'BINARY_SHA':sha(ROOT/'p95-priority')}.items():
        text=text.replace('@'+key+'@',value)
    assert not re.search(r'@[A-Z_]+@',text)
    script=args.output/'p95-priority-setup.sh'
    script.write_text(text.rstrip('\n')+'\n'+base64.encodebytes(archive.read_bytes()).decode('ascii'),encoding='ascii',newline='\n')
    subprocess.run([args.bash, str(script), '--version'],check=True)
    subprocess.run([args.bash, str(script), '--help'],check=True,stdout=subprocess.DEVNULL)
    for path in (archive,script):
        Path(str(path)+'.sha256').write_text(sha(path)+'  '+path.name+'\n',encoding='ascii',newline='\n')
    with tarfile.open(archive,'r:gz') as tf:
        for line in manifest.splitlines():
            digest,name=line.split('  ',1)
            assert hashlib.sha256(tf.extractfile('p95-priority-helper/'+name).read()).hexdigest()==digest
    print(json.dumps({'version':version,'script':str(script),'script_bytes':script.stat().st_size,
                      'script_sha256':sha(script),'archive':str(archive)},indent=2))


if __name__=='__main__':
    main()
