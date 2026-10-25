# Capacity is read only after serial/scope verification. Linux sysfs reports
# 512-byte sectors even on 4Kn devices. RAID1 uses the smaller member, never sum.
# Disk tiers use decimal vendor GB; filesystem allocations below use MiB/GiB.
vc_storage_error() { echo "Ventoy storage: $*" >&2; return 1; }
# partman recipes take decimal MB, unlike Kickstart (MiB) and Curtin (bytes).
# Round up to a whole MB; native partition/LVM alignment may add a few MiB.
vc_mib_to_mb() { printf '%s\n' "$((($1 * 1048576 + 999999) / 1000000))"; }
vc_disk_bytes() (
    case "$1" in ''|*[!a-zA-Z0-9_-]*) exit 1;; esac
    vc_sectors=$(cat "/sys/class/block/$1/size") || exit 1
    case "$vc_sectors" in ''|0*|*[!0-9]*) exit 1;; esac
    # Bound input before shell arithmetic (well above supported drive sizes).
    [ "${#vc_sectors}" -le 14 ] || exit 1
    printf '%s\n' "$((vc_sectors * 512))"
)
vc_storage_sizes() {
    vc_capacity=$(vc_disk_bytes "$1") || { vc_storage_error 'Cannot read verified disk capacity'; return 1; }
    if [ "$#" = 2 ]; then
        vc_second_capacity=$(vc_disk_bytes "$2") || { vc_storage_error 'Cannot read second disk capacity'; return 1; }
        if [ "$vc_second_capacity" -lt "$vc_capacity" ]; then vc_capacity=$vc_second_capacity; fi
    fi
    [ "$vc_capacity" -ge 120000000000 ] || { vc_storage_error 'Default partition layout requires at least 120 GB per disk'; return 1; }
    vc_quarters=1
    vc_next_tier=240000000000
    while [ "$vc_capacity" -ge "$vc_next_tier" ]; do
        vc_quarters=$((vc_quarters * 2))
        vc_next_tier=$((vc_next_tier * 2))
    done
    vc_tmp_mib=$((1280 * vc_quarters))
    vc_swap_mib=$((8192 * vc_quarters))
    vc_var_mib=$((16384 * vc_quarters))
    vc_root_mib=$((32768 * vc_quarters))
    # Leave room for GPT/alignment/MD/LVM metadata and at least 8 GiB /home.
    vc_required_mib=$((1024 + 2048 + vc_tmp_mib + vc_swap_mib + vc_var_mib + vc_root_mib + 256 + 8192))
    [ "$((vc_capacity / 1048576))" -ge "$vc_required_mib" ] || { vc_storage_error 'Insufficient capacity for the selected layout and /home'; return 1; }
    VC_STORAGE_SIZES_MIB="$vc_tmp_mib $vc_swap_mib $vc_var_mib $vc_root_mib"
    export VC_STORAGE_SIZES_MIB
    printf 'Ventoy storage (MiB): EFI=1024 boot=2048 tmp=%s swap=%s var=%s root=%s home=remaining; capacity=%s bytes\n' "$vc_tmp_mib" "$vc_swap_mib" "$vc_var_mib" "$vc_root_mib" "$vc_capacity"
}
vc_storage_sizes "$disk"{{if .StorageGuard}} "$disk2"{{end}}
