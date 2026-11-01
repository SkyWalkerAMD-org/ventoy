#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
# Ventoy adapter for the unmodified SkyWalkerAMD/E610 r4 offline release.
set -Eeuo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C
umask 077
mode=${1:---check}; bundle=${2:?E610 bundle directory required}
kver=$(uname -r)
die() { echo "E610: $*" >&2; exit 1; }
[[ $mode == --apply || $mode == --check ]] || exit 2
[[ $EUID == 0 && $(uname -m) == x86_64 && $kver =~ ^[0-9][a-zA-Z0-9._+-]*$ ]] || die 'Requires root and an x86_64 installed kernel.'
devices=()
for pci in /sys/bus/pci/devices/*; do
    [[ $(cat "$pci/vendor") == 0x8086 ]] || continue
    case "$(cat "$pci/device")" in 0x57ae|0x57af|0x57b0|0x57b1|0x57b2) devices+=("$pci");; esac
done
if ((${#devices[@]} == 0)); then echo 'E610_NOT_PRESENT: no E610 hardware; no driver replaced.'; exit 0; fi
native_support() {
    local aliases id
    aliases=$(modinfo -k "$kver" -F alias ixgbe 2>/dev/null) || return 1
    for id in 57AE 57AF 57B0 57B1 57B2; do [[ $aliases == *v00008086d0000$id* ]] || return 1; done
}
verify() {
    local pci expected active
    if ! native_support; then
        echo "E610_DRIVER_PENDING: effective ixgbe for $kver lacks E610 support; installation/MOK enrollment is still required."
        return 3
    fi
    expected=$(modinfo -k "$kver" -F srcversion ixgbe)
    active=$(cat /sys/module/ixgbe/srcversion 2>/dev/null || true)
    if [[ -z $expected || $active != "$expected" ]]; then echo 'E610_REBOOT_REQUIRED: installed and loaded driver differ.'; return 3; fi
    for pci in "${devices[@]}"; do
        if [[ $(basename "$(readlink -f "$pci/driver" 2>/dev/null)") != ixgbe || ! -d $pci/net ]]; then
            echo "E610_PENDING: ${pci##*/} is not bound to ixgbe with a network interface."; return 3
        fi
        echo "E610_DRIVER_VERIFIED pci=${pci##*/} interfaces=$(ls "$pci/net") kernel=$kver"
    done
}
if [[ $mode == --check ]]; then verify; exit $?; fi
if ! native_support; then
    # The r4 dispatcher owns OS normalization, exact ABI selection and nested
    # baseline/imported payloads. Do not flatten its manifest or bypass its
    # checksum, Secure Boot, backup, and rollback checks.
    [[ -f $bundle/install.sh && -f $bundle/SHA256SUMS ]] || die 'Offline driver bundle is incomplete.'
    (cd "$bundle" && sha256sum --status -c SHA256SUMS) || die 'Offline driver bundle checksum mismatch.'
    log=$(mktemp /var/log/ventoy-e610-offline.XXXXXXXX.log)
    echo "E610_OFFLINE: checking exact OS/kernel support for $kver; details: $log"
    echo "E610_ROLLBACK: bash $bundle/install.sh --kernel $kver --rollback"
    rc=0
    bash "$bundle/install.sh" --kernel "$kver" --check > "$log" 2>&1 || rc=$?
    if ((rc == 3)); then
        echo "E610_MOK_PENDING: signing certificate enrollment is required. At a console, run bash $bundle/install.sh --kernel $kver, enroll at reboot, then rerun setup. Details: $log"
        exit 3
    fi
    ((rc == 0)) || die "Offline driver preflight failed ($rc); inspect $log. The current kernel and driver were retained."
    rc=0
    bash "$bundle/install.sh" --kernel "$kver" >> "$log" 2>&1 || rc=$?
    if ((rc == 3)); then
        echo "E610_MOK_PENDING: driver installation needs console enrollment; inspect $log."
        exit 3
    fi
    ((rc == 0)) || die "Offline driver installation failed ($rc); inspect $log and the recorded rollback state."
fi
if ! native_support; then echo 'E610_DRIVER_PENDING: the effective driver still lacks E610 support; inspect the offline installation log.'; exit 3; fi
# Loading an absent driver is safe; never unload/rebind a driver carrying SSH.
[[ -d /sys/module/ixgbe ]] || modprobe ixgbe
udevadm settle --timeout=10
verify
