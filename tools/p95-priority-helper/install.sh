#!/bin/bash
# p95-priority independent helper installer 1.0.3
set -euo pipefail
umask 077

action=install
if [[ ${1:-} == --print-unit ]]; then action=print-unit; shift; fi
product=${1:-fyhocsys}
case "$product" in fyhocsys|sckocstp) ;; *) echo 'Usage: bash install.sh [fyhocsys|sckocstp]' >&2; exit 1 ;; esac
unit="p95-priority-${product}.service"
binary="/usr/local/sbin/p95-priority-${product}"
systemd_info=$(systemctl --version)
read -r systemd_name systemd_version _ <<< "$systemd_info"
[[ "$systemd_name" == systemd && "$systemd_version" =~ ^[0-9]+$ && "$systemd_version" -ge 219 ]] || { echo 'systemd 219 or later is required.' >&2; exit 1; }
render_unit() {
    cat <<EOF
# p95-priority independent helper
[Unit]
Description=Independent P95 thread priority helper for $product
After=$product-node.service

[Service]
Type=notify
NotifyAccess=main
User=root
Group=root
UMask=0077
Environment=GOMAXPROCS=2
ExecStartPre=$binary selfcheck
ExecStart=$binary watch --product $product
Restart=on-failure
RestartSec=5
TimeoutStartSec=30
TimeoutStopSec=15
EOF
    # Request the CPU controller with its default weight. systemd also realizes
    # it for sibling units (including the node service). This makes all workers
    # in the node's CPU cgroup share by thread nice, instead of giving each
    # setsid() memtester its own autogroup against one multi-threaded P95 group.
    # Keep this on the independent helper; never rewrite/restart the node unit
    # or disable autogroup for the whole machine.
    if (( systemd_version >= 232 )); then
        printf 'CPUWeight=100\n'
    fi
    # Legacy cgroup v1 / systemd 219 uses the equivalent neutral shares value.
    printf 'CPUShares=1024\n'
    # RuntimeDirectoryPreserve appeared after EL7. Without it, systemd removes
    # the undo journal when a crashed helper restarts. On 219 the helper itself
    # creates/checks its root-owned 0700 /run directory and preserves it until
    # reboot, including across a crash. Do not use RuntimeDirectory alone.
    if (( systemd_version >= 235 )); then
        cat <<EOF
RuntimeDirectory=p95-priority-$product
RuntimeDirectoryMode=0700
RuntimeDirectoryPreserve=yes
EOF
    fi
    cat <<EOF
CapabilityBoundingSet=CAP_SYS_NICE CAP_SYS_PTRACE
NoNewPrivileges=yes
ProtectSystem=full
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
EOF
}
if [[ $action == print-unit ]]; then render_unit; exit 0; fi
[[ $EUID -eq 0 ]] || { echo 'Run as root on the TEST NODE.' >&2; exit 1; }
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || { echo 'This package requires Linux x86_64.' >&2; exit 1; }
package_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
cd -- "$package_dir"
sha256sum --check --strict SHA256SUMS
./p95-priority preflight --product "$product"
./p95-priority selfcheck

unit="p95-priority-${product}.service"
unit_path="/etc/systemd/system/$unit"
# Separate binary per product, so installing a second product cannot replace
# the binary of a running helper for the first one.
binary="/usr/local/sbin/p95-priority-${product}"
[[ ! -L "$unit_path" && ! -L "$binary" ]] || { echo 'Existing helper paths must not be symlinks.' >&2; exit 1; }
if [[ -e "$unit_path" ]] && ! grep -qx '# p95-priority independent helper' "$unit_path"; then
    echo "Refusing to replace an unrelated unit: $unit_path" >&2; exit 1
fi
if [[ -e "$binary" ]]; then
    installed_version=$("$binary" --version)
    [[ "$installed_version" == 'p95-priority-helper '* ]] || { echo "Refusing to replace unrelated file: $binary" >&2; exit 1; }
fi

install -d -m 0700 /var/backups/p95-priority-helper
backup=$(mktemp -d /var/backups/p95-priority-helper/install.XXXXXXXX)
old_binary=0
old_unit=0
was_active=0
was_enabled=0
if [[ -e "$binary" ]]; then cp -a -- "$binary" "$backup/binary"; old_binary=1; fi
if [[ -e "$unit_path" ]]; then cp -a -- "$unit_path" "$backup/unit"; old_unit=1; fi
if systemctl is-active --quiet "$unit"; then was_active=1; fi
if systemctl is-enabled --quiet "$unit" 2>/dev/null; then was_enabled=1; fi
printf 'Helper backup: %s\n' "$backup"

rollback() {
    rc=$?
    trap - ERR
    set +e
    systemctl stop "$unit" >/dev/null 2>&1
    systemctl disable "$unit" >/dev/null 2>&1
    if [[ $old_binary == 1 ]]; then cp -a -- "$backup/binary" "$binary"; else rm -f -- "$binary"; fi
    if [[ $old_unit == 1 ]]; then cp -a -- "$backup/unit" "$unit_path"; else rm -f -- "$unit_path"; fi
    systemctl daemon-reload
    if [[ $was_enabled == 1 ]]; then systemctl enable "$unit"; fi
    if [[ $was_active == 1 ]]; then systemctl start "$unit"; fi
    printf 'ERROR: helper installation failed (exit %s). Previous helper files restored from %s.\n' "$rc" "$backup" >&2
    printf 'Inspect: journalctl -u %s -n 40 --no-pager\n' "$unit" >&2
    exit "$rc"
}
trap rollback ERR

if [[ $old_unit == 1 ]]; then systemctl stop "$unit"; fi
install -d -m 0755 /usr/local/sbin
install -m 0755 ./p95-priority "${binary}.new"
mv -f -- "${binary}.new" "$binary"
render_unit > "$backup/$unit"
systemd-analyze verify "$backup/$unit"
install -m 0644 "$backup/$unit" "$unit_path"
systemctl daemon-reload
systemctl enable "$unit"
systemctl start "$unit"
systemctl is-active --quiet "$unit"
"$binary" check-scheduling --product "$product"
# Type=notify becomes active only after this invocation completes its first scan.
journalctl -u "$unit" -n 8 --no-pager
trap - ERR
printf '\nINSTALLED on this NODE: %s\n' "$unit"
printf 'Default P95 worker nice 19 is changed to 0; new workers are checked every second.\n'
printf 'The helper requests default CPU-controller weights; active node CPU-group membership is checked.\n'
printf 'Start your mixed test in the center, then run:\n  %s status --product %s --seconds 10\n' "$binary" "$product"
printf 'Disable and restore recorded priorities:\n  systemctl stop %s\n  systemctl disable %s\n' "$unit" "$unit"
printf 'No test was started. Workload performance still needs a mixed-run check.\n'
