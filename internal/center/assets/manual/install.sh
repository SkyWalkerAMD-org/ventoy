#!/usr/bin/env bash
# Ventoy Center manual-install setup. Run after booting the installed OS.
set -Eeuo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C SYSTEMD_PAGER=cat
umask 077
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
state=/var/lib/ventoy-center/manual-init
mode=${1:---help}; (($# == 0)) || shift
name=; offline=0; keep_passwords=0
die() { echo "Manual setup: $*" >&2; exit 1; }
while (($#)); do
    case "$1" in
        --hostname) (($# >= 2)) || exit 2; name=$2; shift;;
        --offline) offline=1;;
        --keep-passwords) keep_passwords=1;;
        *) die "Unknown argument: $1";;
    esac
    shift
done
case "$mode" in
    --apply|--resume|--check) ;;
    --help|-h)
        cat <<'HELP'
sudo bash install.sh --apply [--hostname NAME] [--offline] [--keep-passwords]
sudo bash /opt/ventoy-manual/current/install.sh --check

Installs E610 support when required; enables root/admin SSH password login;
configures unconfigured wired NICs for DHCP and activates new profiles;
locks the installed kernel, disables automatic updates, and verifies results.
Default initial credentials: admin / ventoy, root / ventoy.
Existing users and configured static networks are preserved. --keep-passwords
retains existing account passwords (root must already have an unlocked password).
Missing packages may be installed, but no system/kernel upgrade is performed.
--offline prohibits package downloads. E610 driver activation may need a reboot;
the installed service resumes setup and verifies it on subsequent boots.
Exit 0 = verified, 1 = failed, 2 = invalid arguments, 3 = pending/reboot required.
HELP
        exit 0;;
    *) exit 2;;
esac
[[ $EUID == 0 && -d /run/systemd/system ]] || die 'Run as root in the installed, booted system.'
[[ $(stat -Lc '%d:%i' /) == $(stat -Lc '%d:%i' /proc/1/root) ]] || die 'Do not run in a chroot.'
[[ ! -e /run/anaconda && ! -e /run/casper && ! -e /lib/debian-installer ]] || die 'Do not run in the live installer.'
case "$(findmnt -n -o FSTYPE /)" in overlay|aufs|squashfs|tmpfs|ramfs) die 'Live/container root is not supported.';; esac
. /etc/os-release
case "$ID" in
    ubuntu|debian) family=apt;;
    centos|rocky|almalinux|rhel|ol|scientific|anolis) family=rpm;;
    *) die "Unsupported Linux distribution: $ID";;
esac
mkdir -p "$state"
exec 7>"$state/lock"
flock -n 7 || die 'Another setup/check is running.'
if [[ $mode == --apply ]]; then
    (cd "$here" && sha256sum --status -c SHA256SUMS) || die 'Package integrity check failed.'
    # Keep a complete offline copy for automatic post-reboot continuation.
    release=$(sha256sum "$here/SHA256SUMS" | cut -c1-16)
    dest=/opt/ventoy-manual/$release
    [[ ! -L /opt/ventoy-manual && ! -L "$dest" ]] || die 'Unexpected package directory symlink.'
    if [[ "$here" != "$dest" ]]; then
        mkdir -p /opt/ventoy-manual
        if [[ ! -e "$dest" ]]; then
            tmp=$(mktemp -d /opt/ventoy-manual/.stage.XXXXXXXX)
            cp -a "$here/." "$tmp/"
            (cd "$tmp" && sha256sum --status -c SHA256SUMS) || die 'Installed package copy failed validation.'
            mv -T "$tmp" "$dest"
        fi
        (cd "$dest" && sha256sum --status -c SHA256SUMS) || die 'Stored package was modified.'
    fi
    ln -sfn "$dest" /opt/ventoy-manual/current.new
    mv -Tf /opt/ventoy-manual/current.new /opt/ventoy-manual/current
    args=(--resume)
    [[ -z "$name" ]] || printf '%s\n' "$name" > "$state/hostname-request"
    printf '%s\n' "$offline" > "$state/offline"
    printf '%s\n' "$keep_passwords" > "$state/keep-passwords"
    install -m 0644 "$dest/ventoy-manual-init.service" /etc/systemd/system/ventoy-manual-init.service
    if command -v restorecon >/dev/null; then
        restorecon -R /opt/ventoy-manual /etc/systemd/system/ventoy-manual-init.service
    fi
    systemctl daemon-reload
    systemctl enable ventoy-manual-init.service
    systemctl reset-failed ventoy-manual-init.service || true
    flock -u 7
    exec bash "$dest/install.sh" "${args[@]}"
fi
[[ -f "$state/offline" && -f "$state/keep-passwords" ]] || die 'Run --apply first.'
offline=$(cat "$state/offline"); keep_passwords=$(cat "$state/keep-passwords")
export VC_MANUAL_OFFLINE=$offline
exec > >(tee -a /var/log/ventoy-manual-init.log) 2>&1
printf '\n[%s] mode=%s os=%s/%s kernel=%s\n' "$(date -Is)" "$mode" "$ID" "$VERSION_ID" "$(uname -r)"
trap 'rc=$?; flock -u 7; if ((rc != 0 && rc != 3)); then printf "failed exit=%s time=%s\n" "$rc" "$(date -Is)" > "$state/status"; fi' EXIT
printf 'running %s\n' "$(date -Is)" > "$state/status"
pending=0
if [[ $mode == --resume ]]; then
    # A console/SSH login can become available before Ubuntu's first-boot
    # cloud-config job finishes writing SSH and hostname settings. Join that
    # already queued job before applying our explicitly requested settings.
    # Do not start disabled cloud-init, or wait for cloud-final: the latter is
    # ordered after multi-user.target and would deadlock our resume service.
    if [[ $ID == ubuntu ]]; then
        jobs=$(systemctl list-jobs --no-legend --no-pager) || die 'Cannot inspect first-boot initialization.'
        if grep -Eq '(^|[[:space:]])cloud-config\.service([[:space:]]|$)' <<< "$jobs"; then
            echo 'Waiting for the queued Ubuntu cloud-config job before configuring the installed system.'
            timeout 180 systemctl start cloud-config.service || die 'Ubuntu cloud-config did not finish; retry setup after initialization completes.'
        fi
    fi
    # The driver step never unloads a live NIC driver or upgrades the kernel.
    rc=0; bash "$here/e610-driver.sh" --apply "$here/e610" || rc=$?
    case "$rc" in 0) ;; 3) pending=1;; *) die "E610 preparation failed (exit $rc); see log.";; esac
    name=$(cat /etc/hostname)
    [[ ! -f "$state/hostname" ]] || name=$(cat "$state/hostname")
    [[ ! -f "$state/hostname-request" ]] || name=$(cat "$state/hostname-request")
    name=${name,,}
    case "$name" in ''|localhost|localhost.localdomain) name=linux-$(cut -c1-8 /etc/machine-id);; esac
    if [[ ! -f "$state/hostname" || $(cat /etc/hostname) != "$name" || $(hostname) != "$name" ]]; then
        bash "$here/safe-server-init.sh" hostname "$name" --update-hosts
    fi
    cat /etc/hostname > "$state/hostname"
    if [[ -d /etc/cloud/cloud.cfg.d ]]; then
        printf 'preserve_hostname: true\n' > /etc/cloud/cloud.cfg.d/99-ventoy-manual-hostname.cfg
    fi
    if [[ ! -e "$state/configured" ]]; then
        if ! command -v python3 >/dev/null && ! command -v python >/dev/null && [[ ! -x /usr/libexec/platform-python ]]; then
            [[ $offline == 0 ]] || die 'Python is required for safe network parsing; install it from the OS media.'
            if [[ $family == apt ]]; then
                apt-get -o Acquire::Retries=1 -o Acquire::http::Timeout=20 update
                DEBIAN_FRONTEND=noninteractive apt-get -y --no-upgrade --no-install-recommends install python3
            else
                manager=yum; command -v dnf >/dev/null && manager=dnf
                "$manager" -y --exclude='kernel*' install python3
            fi
        fi
        # DHCP can make package installation possible on an otherwise offline host.
        rc=0; bash "$here/manual-network.sh" --apply 7>&- || rc=$?
        case "$rc" in 0) ;; 3) pending=1;; *) die "Network setup failed (exit $rc).";; esac
        rc=0; bash "$here/manual-ssh.sh" --apply "$keep_passwords" || rc=$?
        if ((rc != 0)); then
            # Persist the requested update policy even if a missing SSH package
            # must wait until the E610 driver is active after reboot.
            sh "$here/linux-update-policy.sh" --apply
            die "SSH setup/check failed (exit $rc); inspect the log. Kernel/update policy was applied."
        fi
        sh "$here/linux-update-policy.sh" --apply
        systemctl set-default multi-user.target
        install -m 0755 "$here/safe-server-init.sh" /usr/local/sbin/safe-server-init
        if command -v restorecon >/dev/null; then restorecon /usr/local/sbin/safe-server-init; fi
        touch "$state/configured"
    else
        # A newly available E610 interface after reboot needs DHCP once.
        rc=0; bash "$here/manual-network.sh" --apply 7>&- || rc=$?
        case "$rc" in 0) ;; 3) pending=1;; *) die "Network setup failed (exit $rc).";; esac
    fi
fi
[[ -f "$state/configured" ]] || die 'Initial configuration has not completed.'
[[ $(cat /etc/hostname) == "$(cat "$state/hostname")" && $(hostname) == "$(cat "$state/hostname")" ]] || die 'Hostname verification failed.'
sh "$here/linux-update-policy.sh" --check
bash "$here/manual-ssh.sh" --check "$keep_passwords"
rc=0; bash "$here/manual-network.sh" --check || rc=$?
case "$rc" in 0) ;; 3) pending=1;; *) die "Network verification failed (exit $rc).";; esac
rc=0; bash "$here/e610-driver.sh" --check "$here/e610" || rc=$?
case "$rc" in 0) ;; 3) pending=1;; *) die "E610 verification failed (exit $rc).";; esac
[[ $(systemctl get-default) == multi-user.target ]] || die 'CLI boot target verification failed.'
if ((pending)); then
    echo 'PENDING: settings saved; driver reboot, firmware enrollment, or DHCP lease is still required.'
    printf 'pending %s\n' "$(date -Is)" > "$state/status"
    exit 3
fi
printf 'verified boot=%s time=%s\n' "$(cat /proc/sys/kernel/random/boot_id)" "$(date -Is)" > "$state/status"
echo 'VERIFIED: SSH, hostname, networking, kernel lock, automatic-update disablement, and E610 status.'
