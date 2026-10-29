# Read-only storage scope verification for an explicitly authorized disk
# rebuild. Run before claiming the job; the native installer partitions disks.
vc_storage_verify() {
vc_scope=$(mktemp -d /tmp/ventoy-storage.XXXXXX)
vc_selected=$vc_scope/selected
vc_nodes=$vc_scope/nodes
: > "$vc_selected"
: > "$vc_nodes"
vc_storage_fail() { echo "Ventoy storage: $*" >&2; exit 1; }
command -v sort >/dev/null || vc_storage_fail 'Installer storage comparison tool is missing'
vc_serial() (
    vc_props=$(udevadm info --query=property --path="/sys/class/block/$1") || exit 1
    vc_value=$(printf '%s\n' "$vc_props" | sed -n 's/^ID_SERIAL_SHORT=//p')
    for vc_file in "/sys/class/block/$1/device/serial" "/sys/class/block/$1/serial"; do
        if [ -z "$vc_value" ] && [ -r "$vc_file" ]; then vc_value=$(cat "$vc_file"); fi
    done
    if [ -z "$vc_value" ] && command -v lsblk >/dev/null; then vc_value=$(lsblk -dn -o SERIAL "/dev/$1"); fi
    printf '%s\n' "$vc_value" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
)
vc_resolve() (
    vc_count=0; vc_match=
    for vc_path in /sys/block/*; do
        vc_name=${vc_path##*/}
        case "$vc_name" in sd*|hd*|vd*|xvd*|nvme*n*) ;; *) continue;; esac
        if [ "$(vc_serial "$vc_name")" = "$1" ]; then vc_count=$((vc_count + 1)); vc_match=$vc_name; fi
    done
    [ "$vc_count" = 1 ] || exit 1
    printf '%s\n' "$vc_match"
)
{{range .Request.StoragePlan.DiskSerials}}vc_member=$(vc_resolve {{sh .}}) || vc_storage_fail 'RAID member serial is missing or ambiguous'
case "$vc_member" in ''|*[!a-zA-Z0-9_-]*) vc_storage_fail 'Invalid member name';; esac
[ -b "/dev/$vc_member" ] || vc_storage_fail 'Member is not a block device'
[ "$(cat "/sys/block/$vc_member/ro")" = 0 ] && [ "$(cat "/sys/block/$vc_member/removable")" = 0 ] || vc_storage_fail 'Read-only or removable member'
vc_path=$(readlink -f "/sys/block/$vc_member")
case "$vc_path" in */usb*/*|*/session*/*|*/virtual/*) vc_storage_fail 'External or network member rejected';; */pci*/*) ;; *) vc_storage_fail 'Unknown member connection';; esac
[ "$(cat "/sys/block/$vc_member/size")" -ge 234375000 ] || vc_storage_fail 'RAID member is smaller than 120 GB'
printf '%s\n' "$vc_member" >> "$vc_selected"
{{end}}
{{if eq .Request.StoragePlan.Mode "raid1-lvm"}}[ "$(sed -n '1p' "$vc_selected")" != "$(sed -n '2p' "$vc_selected")" ] || vc_storage_fail 'RAID members resolve to the same disk'
{{end}}

# Traverse both holders and slaves, including partition parents. This catches
# a mounted LV and any third physical member before native destructive actions.
vc_walk() (
    vc_node=$1
    case "$vc_node" in ''|*[!a-zA-Z0-9_-]*) exit 1;; esac
    [ -r "/sys/class/block/$vc_node/dev" ] || exit 1
    grep -qx "$vc_node" "$vc_nodes" && exit 0
    printf '%s\n' "$vc_node" >> "$vc_nodes"
    for vc_link in "/sys/class/block/$vc_node"/holders/* "/sys/class/block/$vc_node"/slaves/*; do
        [ ! -e "$vc_link" ] || vc_walk "${vc_link##*/}" || exit 1
    done
    if [ -r "/sys/class/block/$vc_node/partition" ]; then
        vc_real=$(readlink -f "/sys/class/block/$vc_node"); vc_parent=${vc_real%/*}
        vc_walk "${vc_parent##*/}" || exit 1
    else
        for vc_partition in "/sys/class/block/$vc_node"/"$vc_node"*; do
            [ ! -r "$vc_partition/partition" ] || vc_walk "${vc_partition##*/}" || exit 1
        done
    fi
)
while read -r vc_member; do vc_walk "$vc_member" || vc_storage_fail 'Cannot resolve storage dependencies'; done < "$vc_selected"

# Inactive VGs may not have holders. Require the native LVM tools and inspect
# every PV of any VG touching the selected disks, including missing PVs.
# VG names are not unique (two old installations often use the same name).
# Follow UUIDs so unrelated same-name storage stays outside the erase scope.
command -v pvs >/dev/null || vc_storage_fail 'Installer LVM inspection tool is missing'
pvs --noheadings --separator '|' -o pv_name,vg_name,vg_uuid > "$vc_scope/pvs" || vc_storage_fail 'Cannot inspect existing volume groups'
: > "$vc_scope/vgs"
: > "$vc_scope/known-pvs"
: > "$vc_scope/vg-identities"
while IFS='|' read -r vc_pv vc_vg vc_vg_uuid; do
    vc_pv=$(printf '%s' "$vc_pv" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    vc_vg=$(printf '%s' "$vc_vg" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    vc_vg_uuid=$(printf '%s' "$vc_vg_uuid" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    if [ -n "$vc_vg" ]; then printf '%s|%s\n' "$vc_vg_uuid" "$vc_vg" >> "$vc_scope/vg-identities"; fi
    vc_dev=$(readlink -f "$vc_pv") || continue
    printf '%s\n' "$vc_dev" >> "$vc_scope/known-pvs"
    [ -n "$vc_vg" ] || continue
    if grep -qx "${vc_dev##*/}" "$vc_nodes"; then
        [ -n "$vc_vg_uuid" ] || vc_storage_fail 'The old volume group identity is missing'
        printf '%s\n' "$vc_vg_uuid" >> "$vc_scope/vgs"
    fi
done < "$vc_scope/pvs"
while read -r vc_node; do
    if [ "$(blkid -o value -s TYPE "/dev/$vc_node" 2>/dev/null || true)" = LVM2_member ]; then
        grep -Fxq "/dev/$vc_node" "$vc_scope/known-pvs" || vc_storage_fail 'An existing LVM member could not be fully inspected'
    fi
done < "$vc_nodes"
while IFS='|' read -r vc_pv vc_vg vc_vg_uuid; do
    vc_pv=$(printf '%s' "$vc_pv" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    vc_vg=$(printf '%s' "$vc_vg" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    vc_vg_uuid=$(printf '%s' "$vc_vg_uuid" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    # A selected old vcvg must not hide another, unrelated destination-name
    # collision. The native RAID recipe creates vcvg after removing old storage.
{{if eq .Request.StoragePlan.Mode "raid1-lvm"}}    if [ "$vc_vg" = vcvg ] && ! grep -Fxq "$vc_vg_uuid" "$vc_scope/vgs"; then
        vc_storage_fail 'The destination volume group name belongs to unselected storage'
    fi
{{end}}    [ -n "$vc_vg_uuid" ] && grep -Fxq "$vc_vg_uuid" "$vc_scope/vgs" || continue
    [ -b "$vc_pv" ] || vc_storage_fail 'The old volume group has a missing member'
    vc_dev=$(readlink -f "$vc_pv")
    vc_walk "${vc_dev##*/}" || vc_storage_fail 'Cannot inspect old volume group members'
done < "$vc_scope/pvs"

# Inactive MD members also need scope checks. Probe the signatures directly,
# not only /proc/mdstat, before allowing the installer to assemble/remove them.
command -v mdadm >/dev/null || vc_storage_fail 'Installer RAID inspection tool is missing'
: > "$vc_scope/md-uuids"
while read -r vc_node; do
    if [ "$(blkid -o value -s TYPE "/dev/$vc_node" 2>/dev/null || true)" = linux_raid_member ]; then
        mdadm --examine --export "/dev/$vc_node" > "$vc_scope/md-info" || vc_storage_fail 'Cannot inspect RAID member'
        vc_uuid=$(sed -n 's/^MD_UUID=//p' "$vc_scope/md-info")
        [ -n "$vc_uuid" ] || vc_storage_fail 'RAID member identity is missing'
        printf '%s\n' "$vc_uuid" >> "$vc_scope/md-uuids"
    fi
done < "$vc_nodes"
if [ -s "$vc_scope/md-uuids" ]; then
    blkid -c /dev/null -t TYPE=linux_raid_member -o device > "$vc_scope/md-members" || vc_storage_fail 'Cannot enumerate RAID members'
    while read -r vc_dev; do
        mdadm --examine --export "$vc_dev" > "$vc_scope/md-info" || vc_storage_fail 'Cannot inspect RAID membership'
        vc_uuid=$(sed -n 's/^MD_UUID=//p' "$vc_scope/md-info")
        [ -n "$vc_uuid" ] || vc_storage_fail 'RAID member identity is missing'
        if grep -Fxq "$vc_uuid" "$vc_scope/md-uuids"; then
            vc_dev=$(readlink -f "$vc_dev"); vc_walk "${vc_dev##*/}" || vc_storage_fail 'Cannot resolve RAID membership'
        fi
    done < "$vc_scope/md-members"
fi
: > "$vc_scope/swaps"
: > "$vc_scope/devices"
while read -r vc_node; do
    vc_slave_count=0
    for vc_slave in "/sys/class/block/$vc_node"/slaves/*; do [ ! -e "$vc_slave" ] || vc_slave_count=$((vc_slave_count + 1)); done
    if [ ! -f "/sys/class/block/$vc_node/partition" ] && [ "$vc_slave_count" = 0 ]; then
        grep -qx "$vc_node" "$vc_selected" || vc_storage_fail "Existing storage spans an unselected disk: /dev/$vc_node (serial $(vc_serial "$vc_node")). Select all member disks for a rebuild; no disk has been erased"
    fi
    vc_devno=$(cat "/sys/class/block/$vc_node/dev")
    printf '%s|%s\n' "$vc_node" "$vc_devno" >> "$vc_scope/devices"
    while read -r vc_mid vc_parent vc_mdev vc_rest; do
        [ "$vc_mdev" != "$vc_devno" ] || vc_storage_fail 'Selected storage is mounted; stopped'
    done < /proc/self/mountinfo
    while read -r vc_swap vc_rest; do
        [ "$vc_swap" = Filename ] && continue
        vc_swap=$(readlink -f "$vc_swap") || vc_storage_fail 'Cannot resolve active swap'
        if [ "$vc_swap" = "/dev/$vc_node" ]; then
            [ -b "$vc_swap" ] || vc_storage_fail 'Selected swap is not a block device'
            command -v swapoff >/dev/null || vc_storage_fail 'Installer swap release tool is missing'
            printf '%s\n' "$vc_swap" >> "$vc_scope/swaps"
        fi
    done < /proc/swaps
done < "$vc_nodes"

# Native partitioners (including partman) still remove VGs by name. Plan a
# UUID-addressed rename only for selected VGs whose name is ambiguous. This
# phase remains read-only; the caller must claim the one-use job before calling
# vc_storage_prepare. The destination name is checked against every old VG.
: > "$vc_scope/renames"
while IFS='|' read -r vc_vg_uuid vc_vg; do
    grep -Fxq "$vc_vg_uuid" "$vc_scope/vgs" || continue
    case "$vc_vg_uuid" in ''|*[!a-zA-Z0-9-]*) vc_storage_fail 'Invalid old volume group identity';; esac
    vc_new_name=vc_rebuild_$(printf '%s' "$vc_vg_uuid" | tr -d '-')
    vc_duplicate=no
    vc_name_used=no
    while IFS='|' read -r vc_other_uuid vc_other_name; do
        if [ "$vc_other_name" = "$vc_vg" ] && [ "$vc_other_uuid" != "$vc_vg_uuid" ]; then vc_duplicate=yes; fi
        if [ "$vc_other_name" = "$vc_new_name" ] && [ "$vc_other_uuid" != "$vc_vg_uuid" ]; then vc_name_used=yes; fi
    done < "$vc_scope/vg-identities"
    if [ "$vc_duplicate" = yes ]; then
        [ "$vc_name_used" = no ] || vc_storage_fail 'Temporary rebuild volume group name is already in use'
        command -v vgrename >/dev/null || vc_storage_fail 'Installer LVM rename tool is missing'
        vc_rename="$vc_vg_uuid|$vc_new_name"
        grep -Fxq "$vc_rename" "$vc_scope/renames" || printf '%s\n' "$vc_rename" >> "$vc_scope/renames"
    fi
done < "$vc_scope/vg-identities"
}
vc_storage_verify
vc_storage_prepare() {
    # Repeat the complete read-only check after the one-use claim. In
    # particular, a new mount or an added VG/MD member must prevent swapoff.
    vc_previous_scope=$vc_scope
    vc_storage_verify
    for vc_snapshot in pvs devices selected; do
        vc_before=$(LC_ALL=C sort "$vc_previous_scope/$vc_snapshot") || vc_storage_fail 'Cannot compare storage'
        vc_after=$(LC_ALL=C sort "$vc_scope/$vc_snapshot") || vc_storage_fail 'Cannot compare storage'
        [ "$vc_before" = "$vc_after" ] || vc_storage_fail 'Storage changed after verification; boot PXE again'
    done
    # Live installers may activate swap from the previous OS. Release only
    # block devices whose entire storage graph belongs to the selected disks.
    # Never use swapoff -a: the installer and other disks may need their swap.
    while read -r vc_swap; do
        echo "Ventoy storage: releasing selected old swap: $vc_swap"
        swapoff "$vc_swap" || vc_storage_fail 'Cannot release selected old swap; free installer RAM and retry'
    done < "$vc_scope/swaps"
    while read -r vc_swap vc_rest; do
        [ "$vc_swap" = Filename ] && continue
        vc_swap=$(readlink -f "$vc_swap") || vc_storage_fail 'Cannot recheck active swap'
        if [ "$vc_swap" != "${vc_swap#/dev/}" ] && grep -Fxq "${vc_swap#/dev/}" "$vc_nodes"; then
            vc_storage_fail 'Selected swap remains active; stopped'
        fi
    done < /proc/swaps
    while IFS='|' read -r vc_vg_uuid vc_new_name; do
        vgrename "$vc_vg_uuid" "$vc_new_name" || vc_storage_fail 'Cannot disambiguate the selected old volume group'
    done < "$vc_scope/renames"
    udevadm settle || vc_storage_fail 'Cannot settle renamed volume groups'
}
disk=$(sed -n '1p' "$vc_selected")
{{if eq .Request.StoragePlan.Mode "raid1-lvm"}}disk2=$(sed -n '2p' "$vc_selected")
echo "Ventoy storage: two-disk rebuild scope verified: /dev/$disk /dev/$disk2"
{{else}}echo "Ventoy storage: single-disk rebuild scope verified: /dev/$disk"
{{end}}
