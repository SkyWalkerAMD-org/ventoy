# Read-only preview; input comes from the same inventory shown to the operator.
vc_preview() (
    capacity=$1; filesystem=$2; firmware=$3; mode=$4
    case "$capacity" in ''|*[!0-9]*) return 1;; esac
    [ "$capacity" -ge 120000000000 ] || { echo 'Default layout requires at least 120 GB per disk'; return 1; }
    quarters=1; threshold=240000000000
    while [ "$capacity" -ge "$threshold" ]; do quarters=$((quarters*2)); threshold=$((threshold*2)); done
    tmp=$((1280*quarters)); swap=$((8192*quarters)); var=$((16384*quarters)); root=$((32768*quarters))
    efi=1024; [ "$firmware" = uefi ] || efi=1
    remaining=$((capacity/1048576-efi-2048-tmp-swap-var-root))
    printf '\nPARTITION PREVIEW - no changes yet\nLayout: %s; capacity basis: %s bytes\n' "$mode" "$capacity"
    if [ "$firmware" = uefi ]; then printf '  /boot/efi  1024 MiB FAT32 (each selected disk)\n'
    else printf '  BIOS boot  1 MiB (each selected disk)\n'; fi
    printf '  /boot  2048 MiB ext4\n  /tmp   %s MiB %s\n  SWAP   %s MiB\n  /var   %s MiB %s\n  /      %s MiB %s\n' "$tmp" "$filesystem" "$swap" "$var" "$filesystem" "$root" "$filesystem"
    printf '  /home  remaining, approximately %s-%s MiB %s\n' "$((remaining-256))" "$remaining" "$filesystem"
    printf 'RAID1 uses the smaller disk. Final sizes vary with alignment and RAID/LVM metadata.\n\n'
)
