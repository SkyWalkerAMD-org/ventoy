#!/bin/sh
# Only run by validate_storage_vm.py in a disposable, networkless QEMU guest.
if [ -r /proc/cmdline ]; then
    echo 'Run validate_storage_vm.py; this script is only an initramfs entrypoint.' >&2
    exit 1
fi
export PATH=/qa/bin:/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C
mkdir -p /proc /sys /dev /run /tmp /target
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
mount -t tmpfs tmpfs /run
exec </dev/console >/dev/console 2>&1
set -eu
finish() { sync; poweroff -f; }
trap 'echo VC_STORAGE_VM_FAILED; finish' EXIT
grep -qw ventoy.storage-test=1 /proc/cmdline
for module in virtio_pci virtio_blk dm_mod raid1 ext4; do modprobe "$module"; done
/lib/systemd/systemd-udevd --daemon
udevadm trigger
udevadm settle
# Refuse accidental execution anywhere except the runner's three test disks.
[ "$(cat /sys/block/vda/serial)" = QA-FIRST ]
[ "$(cat /sys/block/vdb/serial)" = QA-SECOND ]
[ "$(cat /sys/block/vdc/serial)" = KEEP ]
[ "$(cat /sys/block/vdc/size)" = 131072 ]
for disk in vda vdb vdc; do
    printf 'label: gpt\nsize=32MiB,type=L\n' | sfdisk "/dev/$disk"
done
udevadm settle
passed=0
check() {
    name=$1 expected=$2 guard=$3 reason=${4:-}
    before=$(sha256sum /dev/vdc)
    status=0
    /bin/sh -eu "/qa/$guard.sh" >/tmp/guard.log 2>&1 || status=$?
    cat /tmp/guard.log
    if [ "$expected" = allow ]; then
        [ "$status" = 0 ]
    else
        [ "$status" != 0 ]
        grep -Fq "$reason" /tmp/guard.log
    fi
    [ "$before" = "$(sha256sum /dev/vdc)" ]
    passed=$((passed + 1))
    echo "PASS $name (unselected disk SHA-256 unchanged)"
}
check blank-single allow single
check blank-pair allow raid
mkswap /dev/vda1
mkswap /dev/vdc1
swapon /dev/vda1
swapon /dev/vdc1
mkfs.ext4 -q /dev/vdb1
mount /dev/vdb1 /target
dd if=/dev/zero of=/target/vda1 bs=1M count=8
chmod 600 /target/vda1
mkswap /target/vda1
swapon /target/vda1
check partition-swap-preflight allow single
grep -q /dev/vda1 /proc/swaps
check partition-swap-release allow prepare
! grep -q /dev/vda1 /proc/swaps
grep -q /dev/vdc1 /proc/swaps
grep -q /target/vda1 /proc/swaps
swapoff /target/vda1
umount /target
swapoff /dev/vdc1
pvcreate -y /dev/vda1 /dev/vdb1 /dev/vdc1
check orphan-pv allow single
vgcreate oldvg /dev/vda1
lvcreate -L 8M -n root oldvg
mkfs.ext4 -q /dev/oldvg/root
check active-lvm allow single
mount /dev/oldvg/root /target
check mounted-lv deny single 'Selected storage is mounted'
umount /target
mkswap /dev/oldvg/root
swapon /dev/oldvg/root
check active-swap-preflight allow single
grep -q /dev/dm-0 /proc/swaps
check active-swap-release allow prepare
! grep -q /dev/dm-0 /proc/swaps
vgchange -an oldvg
check inactive-lvm allow single
# Restrict VG creation to the independent keep disk, just as when another
# installed system is attached later. Both groups really are named oldvg.
vgcreate --config 'devices { filter = [ "a|^/dev/vdc1$|", "r|.*|" ] }' oldvg /dev/vdc1
pvs -o pv_name,vg_name,vg_uuid
if [ -f /qa/baseline.sh ]; then
    check baseline-duplicate-name deny baseline 'Existing storage spans an unselected disk'
fi
check independent-same-name-single allow single
check independent-same-name-pair allow raid
check repeated-preflight allow single
check authorized-duplicate-name-preparation allow prepare
check repeated-preparation allow prepare
selected_uuid=$(pvs --noheadings -o vg_uuid /dev/vda1 | tr -d ' ')
keep_uuid=$(pvs --noheadings -o vg_uuid /dev/vdc1 | tr -d ' ')
vgrename "$selected_uuid" vcvg
vgrename --config 'devices { filter = [ "a|^/dev/vdc1$|", "r|.*|" ] }' "$keep_uuid" vcvg
check same-destination-single allow single
check same-destination-pair deny raid 'destination volume group name belongs to unselected storage'
# Remove only the disposable keep VG after proving that the guard preserved it.
vgremove -y --select "vg_uuid=$keep_uuid"
pvremove /dev/vdc1
vgrename "$selected_uuid" oldvg
vgextend oldvg /dev/vdb1
check spanning-selected-pair allow raid
check spanning-single deny single 'Existing storage spans an unselected disk'
vgextend oldvg /dev/vdc1
check spanning-third deny raid 'Existing storage spans an unselected disk'
vgremove -y oldvg
pvremove /dev/vda1 /dev/vdb1 /dev/vdc1
mdadm --create /dev/md0 --run --level=1 --raid-devices=2 --assume-clean /dev/vda1 /dev/vdb1
udevadm settle
check active-md-pair allow raid
check active-md-single deny single 'Existing storage spans an unselected disk'
mkswap /dev/md0
swapon /dev/md0
check md-swap-single deny prepare 'Existing storage spans an unselected disk'
grep -q /dev/md0 /proc/swaps
check md-swap-release allow raid-prepare
! grep -q /dev/md0 /proc/swaps
mdadm --stop /dev/md0
check inactive-md-pair allow raid
check inactive-md-single deny single 'Existing storage spans an unselected disk'
mdadm --zero-superblock /dev/vda1 /dev/vdb1
mdadm --create /dev/md0 --run --level=1 --raid-devices=2 --assume-clean /dev/vda1 /dev/vdc1
udevadm settle
check md-third deny raid 'Existing storage spans an unselected disk'
mdadm --stop /dev/md0
echo "VC_STORAGE_VM_PASS $passed"
trap - EXIT
finish
