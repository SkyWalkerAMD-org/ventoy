set -eu
umask 077
{{.VersionCheck}}
udevadm settle
test -d /sys/class/net
matched=0
for nic in /sys/class/net/*/address; do
    if [ "$(cat "$nic")" = {{sh .Machine.MAC}} ]; then matched=1; fi
done
[ "$matched" -eq 1 ] || { echo 'MAC mismatch; installation stopped' >&2; exit 1; }
disk_matches() {
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
# Existing multi-device storage needs an explicit storage plan, not this template.
if lsblk -n -o FSTYPE "/dev/$disk" | grep -Eq 'LVM2_member|linux_raid_member'; then
    echo 'Existing LVM/RAID members require manual storage preparation' >&2; exit 1
fi
{{if eq .Machine.Firmware "uefi"}}[ -d /sys/firmware/efi ] || { echo 'UEFI boot required' >&2; exit 1; }
{{else}}[ ! -d /sys/firmware/efi ] || { echo 'Legacy BIOS boot required' >&2; exit 1; }
{{end}}
{{if eq .Target.OS "ubuntu"}}# Subiquity's serial matcher uses udev ID_SERIAL (often MODEL_SERIAL),
# not lsblk's short SERIAL. Bind the path resolved by the checks above.
python3 - "$disk" <<'VC_UBUNTU_STORAGE'
{{.UbuntuStorage}}
VC_UBUNTU_STORAGE
{{end}}
# CentOS 7's installer omits base64; PEM is safe as a quoted shell literal.
printf '%s' {{sh .CA}} > /tmp/ventoy-center-ca.crt
{{if .CA}}curl --cacert /tmp/ventoy-center-ca.crt{{else}}curl{{end}} --fail --silent --show-error --connect-timeout 10 --max-time 30 \
    -H {{sh (printf "Authorization: Bearer %s" .Token)}} -H 'Content-Type: application/json' \
    --data '{"state":"installing"}' {{sh (printf "%s/event" .Base)}}
