#!/usr/bin/env python
# SPDX-License-Identifier: GPL-2.0-only
"""Extract an already checksum-verified E610 source/legacy payload without tar."""
from __future__ import print_function
import io, os, shutil, sys, tarfile

def extract(source, destination):
    if not os.path.isdir(destination) or os.listdir(destination):
        raise ValueError('Destination must be an empty private directory')
    with open(source,'rb') as f:
        data=f.read(64*1024*1024+1)
    if len(data)>64*1024*1024: raise ValueError('Archive too large')
    if source.endswith('.run'):
        marker=b'\n__E610_PAYLOAD_BELOW__\n'
        if marker not in data: raise ValueError('Unknown E610 launcher format')
        data=data.split(marker,1)[1]
    roots=set(); seen=set(); total=0
    with tarfile.open(fileobj=io.BytesIO(data),mode='r:gz') as archive:
        for member in archive:
            name=member.name.rstrip('/')
            if (not name or '\\' in name or any(p in ('','.','..') for p in name.split('/'))
                    or not (member.isfile() or member.isdir()) or name in seen):
                raise ValueError('Unsafe archive member')
            seen.add(name); roots.add(name.split('/')[0]); total+=member.size
            if member.size<0 or member.size>64*1024*1024 or total>256*1024*1024:
                raise ValueError('Archive too large')
            target=os.path.join(destination,name)
            parent=target if member.isdir() else os.path.dirname(target)
            if not os.path.isdir(parent): os.makedirs(parent)
            if member.isfile():
                with open(target,'wb') as out: shutil.copyfileobj(archive.extractfile(member),out)
                os.chmod(target,member.mode & 0o777)
    if len(roots)!=1: raise ValueError('Expected one archive root')
    return os.path.join(destination,next(iter(roots)))

if __name__=='__main__': print(extract(sys.argv[1],sys.argv[2]))
