#!/bin/bash
# Bound to the two installation serials. Native installers create the arrays;
# this hook verifies their membership and makes both ESPs independently bootable.
set -euo pipefail
umask 077
mode=${1:---check}
case "$mode" in --apply|--check) ;; *) exit 2;; esac
fail() { echo "Ventoy RAID: $*" >&2; exit 1; }
serial_of() {
    local value
    value=$(udevadm info --query=property --path="/sys/class/block/$1" | sed -n 's/^ID_SERIAL_SHORT=//p')
    for file in "/sys/class/block/$1/device/serial" "/sys/class/block/$1/serial"; do
        if [ -z "$value" ] && [ -r "$file" ]; then value=$(cat "$file"); fi
    done
    if [ -z "$value" ]; then value=$(lsblk -dn -o SERIAL "/dev/$1"); fi
    printf '%s\n' "$value" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}
disks=()
{{range .Request.StoragePlan.DiskSerials}}found=()
for device in /sys/block/*; do
    name=${device##*/}
    case "$name" in sd*|hd*|vd*|xvd*|nvme*n*) ;; *) continue;; esac
    if [ "$(serial_of "$name")" = {{sh .}} ]; then found+=("$name"); fi
done
[ "${#found[@]}" = 1 ] || fail 'Installed RAID member is missing or ambiguous'
disks+=("${found[0]}")
{{end}}
[ "${#disks[@]}" = 2 ] && [ "${disks[0]}" != "${disks[1]}" ] || fail 'Invalid member count'
verify_array() {
    local md=$1 member count=0 expected parent real seen=' '
    [ "$(cat "/sys/class/block/$md/md/level")" = raid1 ] || fail 'Expected a RAID1 array'
    [ "$(cat "/sys/class/block/$md/md/raid_disks")" = 2 ] || fail 'Expected two RAID members'
    [ "$(cat "/sys/class/block/$md/md/degraded")" = 0 ] || fail 'RAID array is degraded'
    for member in "/sys/class/block/$md"/slaves/*; do
        [ -e "$member" ] || continue
        # Anaconda may place the secondary ESP after /boot's RAID member.
        # Verify the actual partition parent instead of assuming partition
        # numbers. There must be exactly one member on each authorized disk.
        [ -r "$member/partition" ] || fail 'RAID member is not a partition'
        real=$(readlink -f "$member"); parent=${real%/*}; parent=${parent##*/}
        expected=no
        for disk in "${disks[@]}"; do
            [ "$parent" != "$disk" ] || expected=yes
        done
        [ "$expected" = yes ] || fail 'Unexpected physical RAID member'
        case "$seen" in *" $parent "*) fail 'RAID members share one physical disk';; esac
        seen="$seen$parent "
        count=$((count + 1))
    done
    [ "$count" = 2 ] || fail 'Incomplete RAID member set'
}
root_source=$(readlink -f "$(findmnt -n -o SOURCE /)")
# In the native installer's chroot, the mounted target is the current root.
[ -b /dev/vcvg/root ] && [ "$root_source" = "$(readlink -f /dev/vcvg/root)" ] || fail 'Root is not the requested LVM logical volume'
# EL7 lvs interprets /dev/dm-N as a VG name; use the verified LV name instead.
[ "$(lvs --noheadings -o vg_name,lv_name vcvg/root | xargs)" = 'vcvg root' ] || fail 'Root volume identity mismatch'
pv=$(pvs --noheadings -o pv_name --select vg_name=vcvg | xargs)
[ -b "$pv" ] || fail 'Expected exactly one RAID PV'
data_md=$(readlink -f "$pv"); data_md=${data_md##*/}
boot_md=$(readlink -f "$(findmnt -n -o SOURCE /boot)"); boot_md=${boot_md##*/}
[ "$data_md" != "$boot_md" ] || fail 'Boot and data must use separate arrays'
verify_array "$data_md"
verify_array "$boot_md"
{{if eq .Machine.Firmware "uefi"}}
[ -d /boot/efi/EFI ] || fail 'Native EFI installation is missing'
work=$(mktemp -d /run/ventoy-raid-efi.XXXXXX)
trap 'mountpoint -q "$work/esp" && umount "$work/esp"; rm -rf -- "$work"' EXIT
mkdir "$work/esp" "$work/files"
if [ "$mode" = --apply ]; then
    cp -a /boot/efi/EFI/. "$work/files/"
    # Retain the distribution-signed shim and GRUB. A separate fallback path
    # lets firmware boot either member even without its NVRAM entry.
    vendor=
    for dir in "$work/files"/*; do
        if [ -f "$dir/shimx64.efi" ] && [ -f "$dir/grubx64.efi" ]; then
            [ -z "$vendor" ] || fail 'More than one EFI vendor installation'
            vendor=$dir
        fi
    done
    [ -n "$vendor" ] || fail 'Signed shim/GRUB pair is missing'
    mkdir -p "$work/files/BOOT"
    cp "$vendor/shimx64.efi" "$work/files/BOOT/BOOTX64.EFI"
    cp "$vendor/grubx64.efi" "$work/files/BOOT/grubx64.efi"
    for extra in mmx64.efi grub.cfg; do
        [ ! -f "$vendor/$extra" ] || cp "$vendor/$extra" "$work/files/BOOT/$extra"
    done
fi
for disk in "${disks[@]}"; do
    esps=()
    for partition in "/sys/class/block/$disk"/*/partition; do
        [ -r "$partition" ] || continue
        device=${partition%/partition}; device=/dev/${device##*/}
        if [ "$(blkid -o value -s TYPE "$device" || true)" = vfat ]; then esps+=("$device"); fi
    done
    [ "${#esps[@]}" = 1 ] || fail 'Member ESP is missing or ambiguous'
    esp=${esps[0]}
    # Debian normally mounts only the first ESP. An unmounted secondary ESP
    # is expected here; findmnt's status 1 must not abort under pipefail.
    mounted=$(findmnt -nr -S "$esp" -o TARGET | head -n 1 || true)
    own=no
    if [ -z "$mounted" ]; then
        options=ro; [ "$mode" != --apply ] || options=rw
        mount -o "$options" "$esp" "$work/esp"
        mounted=$work/esp; own=yes
    fi
    if [ "$mode" = --apply ]; then
        mkdir -p "$mounted/EFI"
        cp -a "$work/files/." "$mounted/EFI/"
    fi
    test -s "$mounted/EFI/BOOT/BOOTX64.EFI"
    test -s "$mounted/EFI/BOOT/grubx64.efi"
    if [ "$own" = yes ]; then umount "$work/esp"; fi
done
if [ "$mode" = --apply ]; then
    # Missing either ESP must not send an otherwise bootable mirror to rescue.
    cp -p /etc/fstab /etc/fstab.ventoy-raid-before
    awk '$2=="/boot/efi" || $2=="/boot/efi2" { if($4 !~ /(^|,)nofail(,|$)/) $4=$4 ",nofail,x-systemd.device-timeout=5s" } {print}' /etc/fstab > "$work/fstab"
    cat "$work/fstab" > /etc/fstab
    # Debian/Ubuntu initramfs must explicitly permit a missing mirror member.
    if [ -d /etc/initramfs-tools ]; then
        mkdir -p /etc/initramfs-tools/conf.d
        printf 'BOOT_DEGRADED=true\n' > /etc/initramfs-tools/conf.d/ventoy-raid
        update-initramfs -u -k all
    elif command -v dracut >/dev/null; then
        dracut --regenerate-all --force
    fi
fi
{{else}}
if [ "$mode" = --apply ]; then
    grub=grub-install
    command -v grub2-install >/dev/null && grub=grub2-install
    for disk in "${disks[@]}"; do "$grub" --target=i386-pc "/dev/$disk"; done
fi
{{end}}
echo 'Ventoy RAID: root LVM, both RAID1 arrays and both boot members verified'
