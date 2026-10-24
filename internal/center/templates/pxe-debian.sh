#!/bin/sh
# Debian Installer has BusyBox and udebs, but no Python runtime.
set -eu
umask 077
export LC_ALL=C
fail() {
    echo "Ventoy Center stopped: $*" >&2
    if [ "${console_open:-}" = yes ]; then printf '\nVentoy Center stopped: %s\nNo installation will start. Boot PXE again after correcting this condition.\n' "$*" >&9; fi
    exit 1
}
work=$(mktemp -d /tmp/ventoy-pxe.XXXXXX)
anna-install wget-udeb
wget --version | grep -q 'GNU Wget'
command -v blkid >/dev/null
printf '%s' {{sh .CA}} > "$work/ca.crt"
test -s "$work/ca.crt"
printf 'header = Authorization: Bearer %s\n' {{sh .Token}} > "$work/wget.conf"

json_string() {
    # Model strings can contain spaces and quotes. Inventory names and serials
    # are additionally validated below; no field becomes executable shell text.
    printf '"'
    printf '%s' "$1" | tr '\n\r\t' '   ' | sed 's/\\/\\\\/g;s/"/\\"/g'
    printf '"'
}
boot_mac() {
    for arg in $(cat /proc/cmdline); do
        case "$arg" in BOOTIF=01-*) printf '%s\n' "${arg#BOOTIF=01-}" | tr 'A-F-' 'a-f:'; return;; esac
    done
    # The deployment URL is validated by the server config. ip route accepts
    # the numeric IPv4 endpoint used by this installer network.
    host={{sh .Base}}
    host=${host#https://}; host=${host%%/*}; host=${host%%:*}
    nic=$(ip route get "$host" | sed -n 's/.* dev \([^ ]*\).*/\1/p' | head -n 1)
    case "$nic" in ''|*[!a-zA-Z0-9_.:-]*) fail 'Cannot determine deployment interface';; esac
    cat "/sys/class/net/$nic/address"
}
inventory() {
    udevadm settle
    internal_count=0
    only_serial=
    only_blocked=
    only_location=
    disk_count=0
    mkdir -p "$work/disks"
    printf '{"version":2,"mac":"%s","firmware":"%s","disks":[' "$mac" "$firmware"
    separator=
    for block in /sys/block/*; do
        name=${block##*/}
        case "$name" in loop*|ram*|zram*|sr*|fd*|dm-*|md*) continue;; esac
        case "$name" in ''|*[!A-Za-z0-9_-]*) fail 'Unexpected block device name';; esac
        sectors=$(cat "$block/size")
        [ "$sectors" -gt 0 ] || continue
        properties=$(udevadm info --query=property --path="$block")
        disk_serial=$(printf '%s\n' "$properties" | sed -n 's/^ID_SERIAL_SHORT=//p')
        for fallback in "$block/device/serial" "$block/serial"; do
            if [ -z "$disk_serial" ] && [ -r "$fallback" ]; then
                disk_serial=$(sed 's/^[[:space:]]*//;s/[[:space:]]*$//' "$fallback")
            fi
        done
        connection=$(readlink -f "$block")
        bus=$(printf '%s\n' "$properties" | sed -n 's/^ID_BUS=//p')
        location=unknown
        case "$connection:$bus" in
            */usb[0-9]*/*|*:usb) location=external;;
            */session[0-9]*/*|*/virtual/*|*:iscsi|*:fc|*:fcoe|*:tcp|*:rdma) ;;
            */pci[0-9a-f:]*/*) location=internal;;
        esac
        [ "$(cat "$block/removable")" = 0 ] || location=external
        blocked=
        storage_blocked=
        [ "$location" = internal ] || blocked='External or unknown connection'
        [ "$(cat "$block/ro")" = 0 ] || blocked='Read-only disk'
        case "$disk_serial" in ''|*[!A-Za-z0-9_.:/-]*) blocked='Missing or unsupported disk_serial';; esac
        [ "${#disk_serial}" -le 120 ] || blocked='Unsupported disk_serial length'
        for node in "$block" "$block"/"$name"*; do
            [ -r "$node/dev" ] || continue
            minor=$(cat "$node/dev")
            # Debian 10's installer omits awk. Read the mount device field
            # with shell builtins so missing utilities cannot hide a mount.
            while read -r mount_id parent_id mounted_dev mount_rest; do
                [ "$mounted_dev" != "$minor" ] || blocked='Mounted disk'
            done < /proc/self/mountinfo
            dev="/dev/${node##*/}"
            sed '1d;s/[[:space:]].*//' /proc/swaps > "$work/swaps"
            while read -r swap; do
                [ "$(readlink -f "$swap")" != "$dev" ] || printf '%s\n' "$dev" > "$work/active-swap"
            done < "$work/swaps"
            if [ -f "$work/active-swap" ]; then blocked='Active swap'; rm -f "$work/active-swap"; fi
            for holder in "$node"/holders/*; do [ ! -e "$holder" ] || storage_blocked='Active RAID or LVM'; done
            fs=$(blkid -o value -s TYPE "$dev" || true)
            case "$fs" in LVM2_member|linux_raid_member) storage_blocked='Existing LVM or RAID';; esac
        done
        # A rebuild may replace old LVM/RAID, never a mounted/read-only disk.
        [ -n "$blocked" ] || blocked=$storage_blocked
        model=$(printf '%s\n' "$properties" | sed -n 's/^ID_MODEL=//p' | cut -c1-120)
        disk_count=$((disk_count + 1))
        # Fixed lines are data only, never sourced or evaluated as shell code.
        printf '%s\n' "$name" "$disk_serial" "$((sectors / 2097152))" "$model" "$blocked" "$location" > "$work/disks/$disk_count"
        if [ "$location" != external ]; then
            internal_count=$((internal_count + 1))
            only_serial=$disk_serial
            only_blocked=$blocked
            only_location=$location
        fi
        printf '%s{"name":"%s","serial":' "$separator" "$name"
        json_string "$disk_serial"
        printf ',"model":'; json_string "$model"
        printf ',"size":%s,"blocked":' "$((sectors * 512))"; json_string "$blocked"
        printf ',"location":"%s"}' "$location"
        separator=,
    done
    printf ']}\n'
}
switch_console() {
    if command -v chvt >/dev/null 2>&1; then
        chvt "$1"
    else
        # Debian 10/11 omit chvt too. The Linux console driver's private CSI
        # sequence switches VTs without adding packages to the installer.
        # console_codes(4): ESC [ 12 ; n ] brings console n to the front.
        printf '\033[12;%s]' "$1" >&9
    fi
}
choose_disk() {
    # d-i has no systemd/getty on VT9. Keep input separate from debconf and
    # from early_command's redirected stdin. cdebconf owns fd 3 and preserves
    # original stdio on 4-6; leave those descriptors intact for preseed loading.
    exec 8</dev/tty9 9>/dev/tty9
    console_open=yes
    # Debian 10/11's minimal BusyBox omits stty. An unused VT starts with
    # canonical input and echo; reset it explicitly where stty is available.
    if command -v stty >/dev/null 2>&1; then stty sane <&8; fi
    switch_console 9
    printf '\nVentoy Center - PXE automatic installation\nSystem: %s\nBoot MAC: %s\n\n' {{sh .PXEISO}} "$mac" >&9
    printf 'Select the installation disk. Kernel will be locked; automatic updates disabled.\n' >&9
    index=1
    while [ "$index" -le "$disk_count" ]; do
        row="$work/disks/$index"
        printf '%s. /dev/%s  %s GiB  %s  SERIAL=%s  %s\n' "$index" "$(sed -n '1p' "$row")" "$(sed -n '3p' "$row")" "$(sed -n '4p' "$row")" "$(sed -n '2p' "$row")" "$(sed -n '5p' "$row")" >&9
        index=$((index + 1))
    done
    serial=
    attempt=1
    while [ "$attempt" -le 3 ]; do
        printf '\nSelect disk number, r for TWO-DISK RAID1 + LVM, or q to cancel. Attempt %s/3: ' "$attempt" >&9
        IFS= read -r value <&8 || fail 'Console input closed; no disk selected'
        attempt=$((attempt + 1))
        case "$value" in q|Q) fail 'Installation canceled at the console';; esac
        case "$value" in r|R) choose_raid; return;; esac
        # Match the printed numbers as strings, without arithmetic on input.
        index=1; selected=
        while [ "$index" -le "$disk_count" ]; do
            if [ "$value" = "$index" ]; then selected="$work/disks/$index"; break; fi
            index=$((index + 1))
        done
        if [ -z "$selected" ]; then printf 'Invalid disk number. Nothing has been erased.\n' >&9; continue; fi
        if [ -n "$(sed -n '5p' "$selected")" ] || [ "$(sed -n '6p' "$selected")" != internal ]; then
            printf 'This disk cannot be selected: %s\n' "$(sed -n '5p' "$selected")" >&9
            continue
        fi
        serial=$(sed -n '2p' "$selected")
        break
    done
    [ -n "$serial" ] || fail 'No usable disk selected after 3 attempts'
    printf '\nSelected: /dev/%s  %s GiB  %s  SERIAL=%s\nALL DATA ON THIS DISK WILL BE ERASED. Other disks will not be selected for installation.\n' "$(sed -n '1p' "$selected")" "$(sed -n '3p' "$selected")" "$(sed -n '4p' "$selected")" "$serial" >&9
    attempt=1
    while [ "$attempt" -le 3 ]; do
        printf 'Type yes to erase and install, or no / q to cancel. Attempt %s/3: ' "$attempt" >&9
        IFS= read -r value <&8 || fail 'Console input closed; no erase confirmation'
        value=$(printf '%s' "$value" | tr A-Z a-z)
        case "$value" in
            yes) confirmation="ERASE $serial"; return;;
            no|q) fail 'Installation canceled at the console';;
        esac
        printf 'Please type yes, no or q. Empty Enter does not confirm. Nothing has been erased.\n' >&9
        attempt=$((attempt + 1))
    done
    fail 'Erase confirmation did not match after 3 attempts'
}
choose_raid() {
    raid_attempt=1
    while [ "$raid_attempt" -le 3 ]; do
        printf '\nRAID1 + LVM: enter TWO disk numbers (example: 1 2), or q: ' >&9
        IFS= read -r value <&8 || fail 'Console input closed'
        case "$value" in q|Q) fail 'Installation canceled at the console';; esac
        first_number=; second_number=; extra=
        read -r first_number second_number extra <<VC_MEMBERS
$value
VC_MEMBERS
        first_row=; second_row=; index=1
        while [ "$index" -le "$disk_count" ]; do
            [ "$first_number" != "$index" ] || first_row="$work/disks/$index"
            [ "$second_number" != "$index" ] || second_row="$work/disks/$index"
            index=$((index + 1))
        done
        raid_attempt=$((raid_attempt + 1))
        if [ -z "$first_row" ] || [ -z "$second_row" ] || [ "$first_row" = "$second_row" ] || [ -n "$extra" ]; then
            printf 'Choose two different disk numbers. Nothing has been erased.\n' >&9; continue
        fi
        raid_valid=yes
        for row in "$first_row" "$second_row"; do
            case "$(sed -n '5p' "$row")" in ''|'Existing LVM or RAID'|'Active RAID or LVM') ;; *) raid_valid=no;; esac
            [ "$(sed -n '6p' "$row")" = internal ] && [ "$(sed -n '3p' "$row")" -ge 24 ] || raid_valid=no
            member=$(sed -n '2p' "$row")
            case "$member" in ''|*[!A-Za-z0-9_.:/-]*) raid_valid=no;; esac
        done
        if [ "$raid_valid" != yes ]; then printf 'A selected RAID member is unavailable or too small.\n' >&9; continue; fi
        serial=$(sed -n '2p' "$first_row")
        second_serial=$(sed -n '2p' "$second_row")
        [ "$serial" != "$second_serial" ] || fail 'Duplicate member serials'
        printf '\nALL DATA ON BOTH SELECTED DISKS WILL BE ERASED:\n%s\n%s\nRebuild RAID1 mirror + LVM; capacity approximately the smaller disk.\n' "$serial" "$second_serial" >&9
        attempt=1
        while [ "$attempt" -le 3 ]; do
            printf 'Type yes to erase BOTH selected disks and install, or no / q to cancel: ' >&9
            IFS= read -r value <&8 || fail 'Console input closed'
            value=$(printf '%s' "$value" | tr A-Z a-z)
            case "$value" in
                yes) confirmation="ERASE RAID1 $serial $second_serial"; storage_plan=$(printf ',"storage_plan":{"mode":"raid1-lvm","disk_serials":["%s","%s"]}' "$serial" "$second_serial"); return;;
                no|q) fail 'Installation canceled at the console';;
            esac
            printf 'Empty Enter does not confirm. Type yes, no or q.\n' >&9
            attempt=$((attempt + 1))
        done
        fail 'RAID erase confirmation did not match'
    done
    fail 'No valid RAID member pair selected'
}
api() {
    WGETRC="$work/wget.conf" wget --ca-certificate="$work/ca.crt" --tries=1 --timeout=45 \
        --header='Content-Type: application/json' --header='Accept: text/x-ventoy-preseed' \
        --post-file="$work/request.json" -O "$work/response" {{sh .Base}}/"$1"
}
mac=$(boot_mac | tr A-F a-f)
printf '%s\n' "$mac" | grep -Eq '^[0-9a-f]{2}(:[0-9a-f]{2}){5}$' || fail 'Invalid boot MAC'
grep -qx "$mac" /sys/class/net/*/address || fail 'Boot MAC does not belong to this machine'
firmware=bios; [ ! -d /sys/firmware/efi ] || firmware=uefi
inventory > "$work/before.json"
cp "$work/before.json" "$work/request.json"
api discover
ticket=$(sed -n 's/^ticket=//p' "$work/response")
serial=$(sed -n 's/^preset_disk=//p' "$work/response")
automatic=$(sed -n 's/^automatic_disk=//p' "$work/response")
policy=$(sed -n 's/^disk_policy=//p' "$work/response")
case "$policy" in single-internal|single-or-console) ;; *) fail 'Disk policy changed; boot the current entry again';; esac
printf '%s\n' "$ticket" | grep -Eq '^[0-9a-f]{48}$' || fail 'Invalid authorization ticket'
confirmation=
storage_plan=
if [ -z "$serial" ]; then
    if [ "$policy" = single-or-console ] && [ "$internal_count" -gt 1 ]; then
        [ -z "$automatic" ] || fail 'Controller and client selected different disks'
        choose_disk
    else
        [ "$internal_count" -eq 1 ] && [ "$only_location" = internal ] && [ -z "$only_blocked" ] || fail 'A single usable internal disk is required'
        [ "$automatic" = "$only_serial" ] || fail 'Controller and client selected different disks'
        serial=$automatic
    fi
fi
case "$serial" in ''|*[!A-Za-z0-9_.:/-]*) fail 'No authorized target disk';; esac
[ "${#serial}" -le 120 ] || fail 'Invalid target serial'
inventory > "$work/rechecked.json"
cmp -s "$work/before.json" "$work/rechecked.json" || fail 'Hardware or mount state changed before authorization'
printf '{"ticket":"%s","disk_serial":"%s","confirmation":"%s"%s}\n' "$ticket" "$serial" "$confirmation" "$storage_plan" > "$work/request.json"
api install
inventory > "$work/rechecked.json"
cmp -s "$work/before.json" "$work/rechecked.json" || fail 'Hardware or mount state changed while fetching configuration'
grep -q '^d-i partman-auto/disk string /dev/ventoy-center-not-authorized$' "$work/response" || fail 'Incomplete preseed response'
# Authentication supplies the native configuration. Its guard independently
# verifies the actual disk and atomically claims the job before permitting writes.
debconf-set-selections "$work/response"
guard=$(debconf-get partman/early_command)
[ -n "$guard" ] || fail 'Missing installation guard'
sh -c "$guard"
if [ -n "$confirmation" ]; then
    printf '\nChecks passed. Automatic installation is starting.\n' >&9
    switch_console 1
    exec 8<&- 9>&-
    console_open=
fi
rm -f "$work/response" "$work/wget.conf"
echo 'Ventoy Center: checks passed; automatic installation is starting.'
