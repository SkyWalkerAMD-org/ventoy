#!/bin/bash
# Offline one-command installer. Source and documentation are inside the payload.
set -euo pipefail
umask 077
release='@VERSION@'
archive_sha='@ARCHIVE_SHA@'
manifest_sha='@MANIFEST_SHA@'
binary_sha='@BINARY_SHA@'
mode=install
product=auto
destination=''
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
while (( $# )); do
    case "$1" in
        --help|-h)
            printf '%s\n' 'Usage: bash p95-priority-setup.sh [fyhocsys|sckocstp]' \
                '       bash p95-priority-setup.sh --check [--product fyhocsys|sckocstp]' \
                '       bash p95-priority-setup.sh --extract NEW_DIRECTORY' \
                'Default: detect the installed node product, install and enable its independent helper.' \
                'Offline. Run installation on the TEST NODE as root; no stress test is started.' \
                'Source and documentation are kept in /usr/local/share/p95-priority-helper/VERSION/.'
            exit 0;;
        --version) printf 'p95-priority-setup %s\n' "$release"; exit 0;;
        --check) [[ $mode == install ]] || die 'Conflicting modes'; mode=check;;
        --extract) [[ $mode == install && $# -ge 2 ]] || die '--extract requires a new directory'; mode=extract; destination=$2; shift;;
        --product) (( $# >= 2 )) || die '--product requires a product'; product=$2; shift;;
        fyhocsys|sckocstp) product=$1;;
        *) die "Unknown argument: $1";;
    esac
    shift
done
case "$product" in auto|fyhocsys|sckocstp) ;; *) die 'Product must be fyhocsys or sckocstp';; esac
self=$(readlink -f -- "${BASH_SOURCE[0]}")
[[ -f $self ]] || die 'Save the script to a file before executing it.'
payload_line=$(awk '/^__P95_PAYLOAD_BELOW__$/ { print NR + 1; exit }' "$self")
[[ $payload_line =~ ^[0-9]+$ ]] || die 'Embedded payload is missing.'

extract_payload() {
    local target=$1 compressed
    [[ ! -e $target && ! -L $target ]] || die "Extraction destination already exists: $target"
    mkdir -m 0700 -- "$target"
    compressed=$target/.payload.tar.gz
    if ! tail -n "+$payload_line" "$self" | base64 --decode > "$compressed"; then die "Payload decoding failed; incomplete directory retained: $target"; fi
    printf '%s  %s\n' "$archive_sha" "$compressed" | sha256sum --check --status --strict - || die 'Embedded archive SHA-256 mismatch; nothing executed.'
    # This archive was produced and pinned with the script. It contains only
    # regular files/directories under p95-priority-helper, never links.
    tar -xzf "$compressed" --no-same-owner -C "$target"
    rm -f -- "$compressed"
    verify_payload "$target/p95-priority-helper"
}

verify_payload() {
    local folder=$1
    [[ -d $folder && ! -L $folder && -f $folder/SHA256SUMS && ! -L $folder/SHA256SUMS ]] || return 1
    printf '%s  %s\n' "$manifest_sha" "$folder/SHA256SUMS" | sha256sum --check --status --strict - || return 1
    (cd -- "$folder" && sha256sum --check --status --strict SHA256SUMS)
}

if [[ $mode == extract ]]; then
    extract_payload "$destination"
    printf 'EXTRACTED: %s/p95-priority-helper (source, binary, installer and documentation; nothing installed).\n' "$destination"
    exit 0
fi

[[ $EUID == 0 ]] || die 'Run as root on the TEST NODE.'
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || die 'Linux x86_64 required.'
if [[ $product == auto ]]; then
    products=()
    for candidate in fyhocsys sckocstp; do
        if { [[ -x /opt/$candidate/workloads/0.1.0/mprime/mprime ]] || \
             [[ -x /opt/$candidate/workloads/0.1.0-el7/mprime/mprime ]]; } && \
           [[ $(systemctl show "$candidate-node.service" --property=LoadState) == LoadState=loaded ]]; then
            products+=("$candidate")
        fi
    done
    (( ${#products[@]} == 1 )) || die 'Cannot uniquely detect the node product. Install the node package first, or pass --product fyhocsys/sckocstp.'
    product=${products[0]}
fi
shared=/usr/local/share/p95-priority-helper
stored=$shared/$release
unit=p95-priority-$product.service
binary=/usr/local/sbin/p95-priority-$product

installed_ok() {
    verify_payload "$stored" || return 1
    [[ -f $binary && ! -L $binary && -f /etc/systemd/system/$unit && ! -L /etc/systemd/system/$unit ]] || return 1
    printf '%s  %s\n' "$binary_sha" "$binary" | sha256sum --check --status --strict - || return 1
    cmp -s /etc/systemd/system/"$unit" <(bash "$stored/install.sh" --print-unit "$product") || return 1
    systemctl is-enabled --quiet "$unit" || return 1
    systemctl is-active --quiet "$unit" || return 1
    "$binary" preflight --product "$product" >/dev/null || return 1
    "$binary" check-scheduling --product "$product" >/dev/null
}

if [[ $mode == check ]]; then
    installed_ok || die 'Helper/service/source verification failed. Run this script without --check to install or repair.'
    printf 'P95_PRIORITY_VERIFIED: %s %s; service enabled/active, binary and retained source verified. No hardware test was performed.\n' "$product" "$release"
    exit 0
fi

systemd_info=$(systemctl --version)
read -r _ systemd_version _ <<< "$systemd_info"
[[ $systemd_version =~ ^[0-9]+$ && $systemd_version -ge 219 ]] || die 'This helper requires systemd 219 or later.'
if installed_ok; then
    printf 'P95_PRIORITY_UNCHANGED: %s %s verified; existing helper kept running.\n' "$product" "$release"
    exit 0
fi
[[ ! -L $shared ]] || die "Unexpected source directory symlink: $shared"
if [[ -e $shared ]]; then
    [[ -d $shared && $(stat -c %u "$shared") == 0 ]] && (( (8#$(stat -c %a "$shared") & 8#22) == 0 )) || die 'Source directory must be root-owned and not group/other writable.'
else
    install -d -m 0755 "$shared"
fi
# Serialize standalone installations without disturbing node services.
exec 8>"$shared/.setup.lock"
flock -n 8 || die 'Another priority-helper installation is in progress.'
if [[ -e $stored || -L $stored ]]; then
    verify_payload "$stored" || die "Retained source differs from this release; preserved for inspection: $stored"
else
    staging=$(mktemp -d "$shared/.stage.XXXXXXXX")
    # extract_payload creates its own directory; the private staging parent is
    # retained on error for diagnosis. Only known empty directories are removed.
    extract_payload "$staging/extracted"
    chmod 0755 "$staging/extracted/p95-priority-helper"
    mv -- "$staging/extracted/p95-priority-helper" "$stored"
    rmdir -- "$staging/extracted" "$staging"
fi
bash "$stored/install.sh" "$product"
installed_ok || die 'Installation returned but the final helper checks failed.'
printf 'P95_PRIORITY_READY: %s %s\nSource and instructions: %s\n' "$product" "$release" "$stored"
printf 'During a mixed test: %s status --product %s --seconds 10\n' "$binary" "$product"
exit 0
__P95_PAYLOAD_BELOW__
