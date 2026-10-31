#!/bin/sh
# Machine-readable observation. Never trust it as an operator's acceptance.
set -u
state=/var/lib/ventoy-center
emit() { printf 'VC_META %s=%s\n' "$1" "$2"; }
emit node_id "$(cat /etc/machine-id 2>/dev/null || true)"
emit attempt_id "$(cat "$state/install-attempt" 2>/dev/null || cat /proc/sys/kernel/random/boot_id)"
emit boot_id "$(cat /proc/sys/kernel/random/boot_id)"
emit hostname "$(hostname)"
emit system "$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release 2>/dev/null | tr -d '\"')"
emit mode "$(cat "$state/install-mode" 2>/dev/null || echo unknown)"
emit release "$(cat "$state/install-release" 2>/dev/null || echo unknown)"
emit cpu_vendor "$(sed -n 's/^vendor_id[[:space:]]*: //p' /proc/cpuinfo | head -n 1)"
emit rootfs "$(findmnt -n -o FSTYPE / 2>/dev/null || true)"
emit raid "$(if grep -q '^md' /proc/mdstat; then echo present; else echo absent; fi)"
emit setup_status "$(head -n 1 "$state/manual-init/status" 2>/dev/null || echo PRELOADED_ONLY)"
emit setup_sha256 "$(sha256sum /root/ventoy-linux-setup.run 2>/dev/null | cut -d ' ' -f 1)"
