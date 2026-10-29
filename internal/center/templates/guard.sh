set -eu
umask 077
{{.FailureShell}}
trap 'vc_result=$?; [ "$vc_result" = 0 ] || vc_install_failed' EXIT
{{.VersionCheck}}
udevadm settle
test -d /sys/class/net
matched=0
for nic in /sys/class/net/*/address; do
    if [ "$(cat "$nic")" = {{sh .Machine.MAC}} ]; then matched=1; fi
done
[ "$matched" -eq 1 ] || { echo 'MAC mismatch; installation stopped' >&2; exit 1; }
{{if .StorageGuard}}{{.StorageGuard}}
{{else}}disk_matches() {
    # CentOS 7's lsblk leaves SERIAL empty for virtio-blk, even when sysfs has it.
    # Examine every whole disk so the fallback cannot hide duplicate serials.
    for candidate in $(lsblk -dn -o NAME,TYPE | awk '$2 == "disk" {print $1}'); do
        serial=$(lsblk -dn -o SERIAL "/dev/$candidate" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        if [ -z "$serial" ] && [ -r "/sys/block/$candidate/serial" ]; then
            serial=$(cat "/sys/block/$candidate/serial" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        fi
        if [ "$serial" = "$1" ]; then printf '%s\n' "$candidate"; fi
    done
}
disk=$(disk_matches {{sh .Machine.DiskSerial}})
[ "$(printf '%s\n' "$disk" | wc -l)" -eq 1 ] && [ -n "$disk" ] || { echo 'Target disk not uniquely identified' >&2; exit 1; }
case "$disk" in *[!a-zA-Z0-9_-]*) echo 'Invalid disk name' >&2; exit 1;; esac
[ -b "/dev/$disk" ] || exit 1
[ "$(lsblk -dn -o TYPE "/dev/$disk" | tr -d ' ')" = disk ] || { echo 'Whole physical disk required' >&2; exit 1; }
[ "$(lsblk -dn -o TRAN "/dev/$disk" | tr -d ' ')" != usb ] || { echo 'USB disk rejected' >&2; exit 1; }
if lsblk -nr -o MOUNTPOINT "/dev/$disk" | grep -q '[^[:space:]]'; then
    echo 'Mounted disk or active swap; installation stopped' >&2; exit 1
fi
# Existing multi-device storage needs an explicit storage plan, not this template.
if lsblk -n -o FSTYPE "/dev/$disk" | grep -Eq 'LVM2_member|linux_raid_member'; then
    echo 'Existing LVM/RAID members require manual storage preparation' >&2; exit 1
fi
{{end}}{{if eq .Machine.Firmware "uefi"}}[ -d /sys/firmware/efi ] || { echo 'UEFI boot required' >&2; exit 1; }
{{else}}[ ! -d /sys/firmware/efi ] || { echo 'Legacy BIOS boot required' >&2; exit 1; }
{{end}}
{{.StorageSizing}}
{{if eq .DataFilesystem "xfs"}}# Check formatter support before claiming the destructive task.
command -v mkfs.xfs >/dev/null || { echo 'XFS formatter is unavailable; no installation will start' >&2; exit 1; }
{{end}}
{{if eq .Target.Installer "autoinstall"}}# Refuse a kernel substitution before claiming the destructive task.
python3 - <<'VC_UBUNTU_KERNEL'
{{.UbuntuKernel}}
VC_UBUNTU_KERNEL
# Subiquity's serial matcher uses udev ID_SERIAL (often MODEL_SERIAL),
# not lsblk's short SERIAL. Bind the path resolved by the checks above.
python3 - "$disk"{{if .RAIDSetup}} "$disk2"{{end}} <<'VC_UBUNTU_STORAGE'
{{.UbuntuStorage}}
VC_UBUNTU_STORAGE
{{else if eq .Target.Installer "ubiquity"}}# Desktop squashfs omits /boot/vmlinuz; Ubiquity copies it from casper.
test -f /cdrom/casper/vmlinuz && test -d "/lib/modules/$(uname -r)" || { echo 'Desktop media kernel or modules missing' >&2; exit 1; }
dpkg-query -W -f='${db:Status-Status}' "linux-image-$(uname -r)" | grep -qx installed
# Resolve the guarded short serial to the exact device before running Ubiquity.
sed -i "s@/dev/ventoy-center-unverified@/dev/$disk@g" /tmp/ventoy-center-selected.cfg
{{.StorageRecipe}}
{{end}}
# CentOS 7's installer omits base64; PEM is safe as a quoted shell literal.
printf '%s' {{sh .CA}} > /tmp/ventoy-center-ca.crt
{{if .Target.Desktop}}python3 - <<'VC_UBI_CLAIM'
import ssl, urllib.request
request = urllib.request.Request({{yaml (printf "%s/event" .Base)}}, data=b'{"state":"installing"}', headers={"Authorization": {{yaml (printf "Bearer %s" .Token)}}, "Content-Type": "application/json"})
with urllib.request.urlopen(request, context=ssl.create_default_context(cafile='/tmp/ventoy-center-ca.crt'), timeout=30) as response:
    response.read()
VC_UBI_CLAIM
{{else}}{{if .CA}}curl --cacert /tmp/ventoy-center-ca.crt{{else}}curl{{end}} --fail --silent --show-error --connect-timeout 10 --max-time 30 \
    -H {{sh (printf "Authorization: Bearer %s" .Token)}} -H 'Content-Type: application/json' \
    --data '{"state":"installing"}' {{sh (printf "%s/event" .Base)}}
{{end}}
{{if .StorageGuard}}# Metadata writes are allowed only after the one-use claim succeeds.
vc_storage_prepare
{{end}}
