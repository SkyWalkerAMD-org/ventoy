#!/bin/sh
# Ventoy Center: run INSIDE the installed system, or its installer chroot.
# No package downloads, upgrades, kernel removals, or repository deletion.
set -eu
export LC_ALL=C
mode=${1:---check}
offline=${2:-}
case "$mode:$offline" in
    --apply:|--apply:--offline|--check:) ;;
    *) echo 'Usage: sh linux-update-policy.sh [--check | --apply [--offline]]' >&2; exit 2;;
esac
[ "$#" -le 2 ] || exit 2
fail() { echo "Kernel/update policy: $*" >&2; exit 1; }
[ -r /etc/os-release ] || fail 'Installed Linux system required'
. /etc/os-release
case "$ID" in
    ubuntu|debian) family=apt;;
    centos|rocky|almalinux|rhel|ol|scientific|anolis) family=rpm;;
    *) fail "Unsupported distribution: $ID";;
esac
if [ "$mode" = --apply ]; then
    [ "$(id -u)" = 0 ] || fail 'Run as root in the installed system'
    if [ -e /run/anaconda ]; then
        # Anaconda 8 bind-mounts /run into the target chroot during %post.
        # Accept that marker only for an explicit offline invocation whose
        # root differs from the running installer's PID 1 root.
        [ "$offline" = --offline ] || fail 'Do not apply to the live installer; enter the installed target first'
        live_root=$(stat -Lc '%d:%i' /proc/1/root 2>/dev/null) || fail 'Do not apply to the live installer; cannot identify installer root'
        [ "$(stat -Lc '%d:%i' /)" != "$live_root" ] || fail 'Do not apply to the live installer; enter the installed target first'
    fi
    # Subiquity 24.04 can leave the ISO mounted at /target/cdrom during
    # late-commands. That mount also appears as /cdrom inside the target
    # chroot, so media presence alone does not identify the live root.
    if [ -e /cdrom/.disk/info ]; then
        case "$(stat -f -c %T /)" in
            ext2/ext3|xfs|btrfs|zfs) ;;
            *) fail 'Do not apply to the live installer; enter the installed target first';;
        esac
    fi
fi
state=/var/lib/ventoy-center/kernel-policy
work=$(mktemp -d /tmp/ventoy-kernel-policy.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
umask 022
kernel_patterns='{{.KernelPackages}}'
if [ "$family" = apt ]; then
    dpkg-query -W -f='${Package}\t${Version}\t${db:Status-Status}\n' > "$work/packages"
    while read -r package version status; do
        [ "$status" = installed ] || continue
        for pattern in $kernel_patterns; do
            case "$package" in $pattern) printf '%s\t%s\n' "$package" "$version"; break;; esac
        done
    done < "$work/packages" | sort -u > "$work/kernel-packages.tsv"
    grep -q '^linux-image-[0-9]' "$work/kernel-packages.tsv" || fail 'No installed versioned kernel image found'
    units='apt-daily.timer apt-daily-upgrade.timer apt-daily.service apt-daily-upgrade.service unattended-upgrades.service'
else
    rpm -qa --qf '%{NAME}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' > "$work/packages"
    awk '$1 ~ /^kernel($|-)/' "$work/packages" | sort -u > "$work/kernel-packages.tsv"
    grep -Eq '^kernel(-core)?[[:space:]]' "$work/kernel-packages.tsv" || fail 'No installed kernel package found'
    units='yum-cron.service dnf-makecache.timer dnf-makecache.service dnf-automatic.timer dnf-automatic.service dnf-automatic-download.timer dnf-automatic-download.service dnf-automatic-install.timer dnf-automatic-install.service dnf-automatic-notifyonly.timer dnf-automatic-notifyonly.service'
fi
if [ -f "$state/kernel-packages.tsv" ]; then
    cmp -s "$state/kernel-packages.tsv" "$work/kernel-packages.tsv" || fail 'Kernel packages differ from the saved lock; review the change before creating a new policy'
fi

if [ "$mode" = --apply ]; then
    mkdir -p "$state"
    # Save originals once. Running this again must not overwrite recovery data.
    if [ "$family" = apt ]; then
        mkdir -p /etc/apt/apt.conf.d /etc/apt/preferences.d
        if [ ! -e "$state/holds-before.txt" ]; then apt-mark showhold > "$state/holds-before.txt"; fi
        cat > "$work/apt.conf" <<'VCAPT'
// Managed by Ventoy Center. Manual apt update/install remain available.
APT::Periodic::Enable "0";
APT::Periodic::Update-Package-Lists "0";
APT::Periodic::Download-Upgradeable-Packages "0";
APT::Periodic::AutocleanInterval "0";
APT::Periodic::Unattended-Upgrade "0";
Unattended-Upgrade::Package-Blacklist { ".*"; };
VCAPT
        : > "$work/kernel.pref"
        while read -r package version; do
            printf 'Package: %s\nPin: version %s\nPin-Priority: 1001\n\n' "$package" "$version" >> "$work/kernel.pref"
        done < "$work/kernel-packages.tsv"
        # Holding only existing packages does not block a NEW kernel ABI name.
        printf 'Package: %s\nPin: version *\nPin-Priority: -1\n' "$kernel_patterns" >> "$work/kernel.pref"
        install -m 0644 "$work/apt.conf" /etc/apt/apt.conf.d/99-ventoy-center-no-auto-updates
        install -m 0644 "$work/kernel.pref" /etc/apt/preferences.d/00-ventoy-center-kernel
        while read -r package version; do apt-mark hold "$package"; done < "$work/kernel-packages.tsv"
        sha256sum /etc/apt/apt.conf.d/99-ventoy-center-no-auto-updates /etc/apt/preferences.d/00-ventoy-center-kernel > "$state/policy.sha256"
    else
        # Preserve unrelated options, repository sections, and existing excludes.
        # On Rocky /etc/yum.conf may point to dnf.conf; retain that symlink.
        for config in /etc/yum.conf /etc/dnf/dnf.conf; do
            [ -f "$config" ] || continue
            real=$(readlink -f "$config")
            backup="$state/$(printf '%s' "$real" | tr / _)"
            [ -e "$backup" ] || cp -p "$real" "$backup"
            awk '
                function finish( count, parts, i, seen, value ) {
                    if (main) {
                        count=split(exclusions " kernel*", parts, /[ ,\t]+/); value=""
                        for (i=1; i<=count; i++) if (parts[i]!="" && !seen[parts[i]]++) value=value " " parts[i]
                        sub(/^ /, "", value); print "exclude=" value
                    }
                    main=0; continuation=0; exclusions=""
                }
                /^[[:space:]]*\[/ { finish(); main=($0 ~ /^[[:space:]]*\[main\][[:space:]]*$/); if (main) found=1; print; next }
                main && /^[[:space:]]*(exclude|excludepkgs)[[:space:]]*=/ {
                    line=$0; sub(/^[^=]*=[[:space:]]*/, "", line); exclusions=exclusions " " line; continuation=1; next
                }
                main && continuation && /^[[:space:]]+[^#;[:space:]]/ { exclusions=exclusions " " $0; next }
                { continuation=0; print }
                END { finish(); if (!found) { print "[main]"; print "exclude=kernel*" } }
            ' "$real" > "$work/rpm.conf"
            # Install via the resolved path; do not replace yum.conf symlinks.
            cat "$work/rpm.conf" > "$real"
        done
        [ -f /etc/yum.conf ] || [ -f /etc/dnf/dnf.conf ] || fail 'No yum/dnf configuration found'
    fi
    for unit in $units; do
        if [ -z "$offline" ] && [ -d /run/systemd/system ]; then
            systemctl stop "$unit" 2>/dev/null || {
                systemctl list-unit-files "$unit" --no-legend | grep -q "$unit" && fail "Could not stop $unit"
                :
            }
        fi
        if [ -e "/usr/lib/systemd/system/$unit" ] || [ -e "/lib/systemd/system/$unit" ] || [ -e "/etc/systemd/system/$unit" ]; then
            systemctl --root=/ disable "$unit"
        fi
        systemctl --root=/ mask "$unit"
    done
    install -m 0644 "$work/kernel-packages.tsv" "$state/kernel-packages.tsv"
    if [ -z "$offline" ] && [ -d /run/systemd/system ]; then systemctl daemon-reload; fi
fi

[ -f "$state/kernel-packages.tsv" ] || fail 'Policy not applied yet; run with --apply'
for unit in $units; do
    [ "$(readlink "/etc/systemd/system/$unit")" = /dev/null ] || fail "$unit is not masked"
    if [ -z "$offline" ] && [ -d /run/systemd/system ]; then
        active=$(systemctl is-active "$unit" 2>/dev/null || true)
        case "$active" in inactive|failed|unknown) ;; *) fail "$unit is still running ($active)";; esac
    fi
done
if [ "$family" = apt ]; then
    sha256sum -c "$state/policy.sha256" >/dev/null || fail 'Managed apt policy was changed'
    apt-config dump > "$work/apt-effective"
    for setting in Enable Update-Package-Lists Download-Upgradeable-Packages AutocleanInterval Unattended-Upgrade; do
        grep -Fqx "APT::Periodic::$setting \"0\";" "$work/apt-effective" || fail "APT periodic $setting is enabled"
    done
    apt-mark showhold > "$work/holds"
    while read -r package version; do grep -Fxq "$package" "$work/holds" || fail "$package is not held"; done < "$work/kernel-packages.tsv"
    grep -Fq 'Pin-Priority: -1' /etc/apt/preferences.d/00-ventoy-center-kernel || fail 'Kernel ABI pin is missing'
else
    for config in /etc/yum.conf /etc/dnf/dnf.conf; do
        [ ! -f "$config" ] || grep -Eq '^exclude=.*kernel\*' "$config" || fail "Kernel exclusion is missing in $config"
    done
fi
printf 'Kernel packages locked; automatic repository refresh and unattended upgrades disabled.\n'
cat "$state/kernel-packages.tsv"
