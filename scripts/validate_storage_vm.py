"""Run the rendered storage guard against real LVM/MD in disposable QEMU disks.

Requires Debian/Ubuntu mkinitramfs, a matching installed kernel, QEMU, lvm2,
mdadm, udev, fdisk and e2fsprogs. No KVM, host block devices or guest network.
This validates preflight and scoped metadata preparation, not a full OS install.
"""
import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


def extract_guard(path, single=True):
    source = path.read_text()
    start = source.index('# Read-only storage scope verification')
    marker = 'echo "Ventoy storage: ' + ('single-disk' if single else 'two-disk')
    end = source.index('\n', source.index(marker, start)) + 1
    return source[start:end]


def run(args):
    kernel = Path(args.kernel).resolve(strict=True)
    version = kernel.name.removeprefix('vmlinuz-')
    if not (Path('/lib/modules') / version).is_dir():
        raise RuntimeError('Matching kernel modules are required for mkinitramfs')
    log = Path(args.log).resolve()
    log.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='ventoy-storage-vm-') as directory:
        work = Path(directory)
        qa = work / 'qa'
        qa.mkdir()
        fixtures = Path(args.fixtures).resolve()
        (qa / 'single.sh').write_text(extract_guard(fixtures / 'single-rebuild/centos-7.9/bios/preinstall.sh'))
        (qa / 'raid.sh').write_text(extract_guard(fixtures / 'raid/ubuntu-22.04/uefi/preinstall.sh', single=False))
        (qa / 'prepare.sh').write_text((qa / 'single.sh').read_text() + '\nvc_storage_prepare\n')
        (qa / 'raid-prepare.sh').write_text((qa / 'raid.sh').read_text() + '\nvc_storage_prepare\n')
        if args.baseline:
            (qa / 'baseline.sh').write_text(extract_guard(Path(args.baseline).resolve() / 'single-rebuild/centos-7.9/bios/preinstall.sh'))
        shutil.copyfile(Path(__file__).with_name('storage_vm_guest.sh'), qa / 'init.sh')
        (qa / 'init.sh').chmod(0o755)
        config = work / 'config'
        shutil.copytree('/etc/initramfs-tools', config)
        (config / 'initramfs.conf').write_text('MODULES=most\nBUSYBOX=y\nCOMPRESS=gzip\n')
        hook = config / 'hooks/ventoy-storage-test'
        hook.parent.mkdir(exist_ok=True)
        # copy_exec preserves the binaries and their shared-library dependencies.
        hook.write_text('''#!/bin/sh
set -eu
case "${1:-}" in prereqs) exit 0;; esac
. /usr/share/initramfs-tools/hook-functions
mkdir -p "$DESTDIR/qa/bin"
for tool in sh bash udevadm sfdisk lsblk blkid sha256sum mkfs.ext4 mkswap swapon swapoff pvs pvcreate pvremove vgcreate vgchange vgextend vgrename vgremove lvcreate mdadm; do
    copy_exec "$(readlink -f "$(command -v "$tool")")" "/qa/bin/$tool"
done
copy_exec /lib/systemd/systemd-udevd
mkdir -p "$DESTDIR/qa"
cp "$VENTOY_VM_WORK"/qa/*.sh "$DESTDIR/qa/"
''')
        hook.chmod(0o755)
        initrd = work / 'initrd.img'
        subprocess.run(['mkinitramfs', '-d', str(config), '-o', str(initrd), version],
                       env=dict(os.environ, VENTOY_VM_WORK=str(work)), check=True)
        # Select our init directly, without system boot services, host mounts
        # or privileged QEMU capabilities. mkinitramfs supplies matching modules.
        command = ['qemu-system-x86_64', '-accel', 'tcg', '-m', '768', '-smp', '2',
                   '-nographic', '-no-reboot', '-nic', 'none', '-kernel', str(kernel),
                   '-initrd', str(initrd), '-append',
                   'console=ttyS0 panic=-1 rdinit=/qa/init.sh ventoy.storage-test=1']
        for index, (serial, size) in enumerate([('QA-FIRST', 128 << 30), ('QA-SECOND', 128 << 30), ('KEEP', 64 << 20)]):
            disk = work / ('disk%d.raw' % index)
            with disk.open('wb') as stream:
                stream.truncate(size)
            command += ['-drive', 'file=%s,format=raw,if=none,id=d%d' % (disk, index),
                        '-device', 'virtio-blk-pci,drive=d%d,serial=%s' % (index, serial)]
        with log.open('wb') as output:
            subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, timeout=args.timeout, check=True)
        result = log.read_text(errors='replace')
        match = re.search(r'^VC_STORAGE_VM_PASS (\d+)\s*$', result, re.M)
        if not match or 'VC_STORAGE_VM_FAILED' in result:
            raise RuntimeError('Storage VM failed; inspect ' + str(log))
        print('Real LVM/MD storage guard: %s checks passed; log: %s' % (match[1], log))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixtures', help='directory exported by VENTOY_TEST_OUTPUT')
    parser.add_argument('--kernel', required=True, help='installed /boot/vmlinuz-VERSION')
    parser.add_argument('--baseline', help='optional old rendered fixtures to reproduce the VG-name bug')
    parser.add_argument('--log', required=True)
    parser.add_argument('--timeout', type=int, default=600)
    run(parser.parse_args())
