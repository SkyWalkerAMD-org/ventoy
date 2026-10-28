# Read-only storage scope verification for an explicitly authorized disk
# rebuild. Run before claiming the job; the native installer performs writes.
vc_scope=$(mktemp -d /tmp/ventoy-storage.XXXXXX)
vc_selected=$vc_scope/selected
vc_nodes=$vc_scope/nodes
: > "$vc_selected"
: > "$vc_nodes"
vc_storage_fail() { echo "Ventoy storage: $*" >&2; exit 1; }
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
command -v pvs >/dev/null || vc_storage_fail 'Installer LVM inspection tool is missing'
pvs --noheadings --separator '|' -o pv_name,vg_name > "$vc_scope/pvs" || vc_storage_fail 'Cannot inspect existing volume groups'
: > "$vc_scope/vgs"
: > "$vc_scope/known-pvs"
while IFS='|' read -r vc_pv vc_vg; do
    vc_pv=$(printf '%s' "$vc_pv" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    vc_vg=$(printf '%s' "$vc_vg" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    vc_dev=$(readlink -f "$vc_pv") || continue
    printf '%s\n' "$vc_dev" >> "$vc_scope/known-pvs"
    [ -n "$vc_vg" ] || continue
    if grep -qx "${vc_dev##*/}" "$vc_nodes"; then printf '%s\n' "$vc_vg" >> "$vc_scope/vgs"; fi
done < "$vc_scope/pvs"
while read -r vc_node; do
    if [ "$(blkid -o value -s TYPE "/dev/$vc_node" 2>/dev/null || true)" = LVM2_member ]; then
        grep -Fxq "/dev/$vc_node" "$vc_scope/known-pvs" || vc_storage_fail 'An existing LVM member could not be fully inspected'
    fi
done < "$vc_nodes"
# Never collide with a VG on an unselected disk, even if no selected member
# currently belongs to that VG. Native installers must not reuse its name.
{{if eq .Request.StoragePlan.Mode "raid1-lvm"}}if grep -Eq '\|[[:space:]]*vcvg[[:space:]]*$' "$vc_scope/pvs" && ! grep -Fxq vcvg "$vc_scope/vgs"; then
    vc_storage_fail 'The destination volume group name belongs to unselected storage'
fi
{{end}}
while IFS='|' read -r vc_pv vc_vg; do
    vc_pv=$(printf '%s' "$vc_pv" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    vc_vg=$(printf '%s' "$vc_vg" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    grep -Fxq "$vc_vg" "$vc_scope/vgs" || continue
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
while read -r vc_node; do
    vc_slave_count=0
    for vc_slave in "/sys/class/block/$vc_node"/slaves/*; do [ ! -e "$vc_slave" ] || vc_slave_count=$((vc_slave_count + 1)); done
    if [ ! -f "/sys/class/block/$vc_node/partition" ] && [ "$vc_slave_count" = 0 ]; then
        grep -qx "$vc_node" "$vc_selected" || vc_storage_fail "Existing storage spans an unselected disk: /dev/$vc_node (serial $(vc_serial "$vc_node")). Select all member disks for a rebuild; no disk has been erased"
    fi
    vc_devno=$(cat "/sys/class/block/$vc_node/dev")
    while read -r vc_mid vc_parent vc_mdev vc_rest; do
        [ "$vc_mdev" != "$vc_devno" ] || vc_storage_fail 'Selected storage is mounted; stopped'
    done < /proc/self/mountinfo
    while read -r vc_swap vc_rest; do
        [ "$vc_swap" = Filename ] && continue
        vc_swap=$(readlink -f "$vc_swap") || vc_storage_fail 'Cannot resolve active swap'
        [ "$vc_swap" != "/dev/$vc_node" ] || vc_storage_fail 'Selected storage has active swap; stopped'
    done < /proc/swaps
done < "$vc_nodes"
disk=$(sed -n '1p' "$vc_selected")
{{if eq .Request.StoragePlan.Mode "raid1-lvm"}}disk2=$(sed -n '2p' "$vc_selected")
echo "Ventoy storage: two-disk rebuild scope verified: /dev/$disk /dev/$disk2"
{{else}}echo "Ventoy storage: single-disk rebuild scope verified: /dev/$disk"
{{end}}
