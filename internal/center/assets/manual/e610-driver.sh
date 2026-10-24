#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
# Canonical source: SkyWalkerAMD-org/E610 manual/e610-driver.sh
set -Eeuo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C
umask 077
mode=${1:---check}; bundle=${2:?E610 bundle directory required}
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
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
    (cd "$bundle" && sha256sum --status -c SHA256SUMS) || die 'Offline driver bundle checksum mismatch.'
    python=
    for candidate in python3 /usr/libexec/platform-python python2 python; do
        if command -v "$candidate" >/dev/null 2>&1; then python=$candidate; break; fi
    done
    [[ -n $python ]] || die 'Python >=2.7 is required to unpack driver payloads.'
    . /etc/os-release
    version=$VERSION_ID; [[ $ID != rocky ]] || version=${VERSION_ID%%.*}
    found=0; kind=; selected=
    while IFS=$'\t' read -r os ver kernel action path; do
        [[ $os == "$ID" && $ver == "$version" && $kernel == "$kver" ]] || continue
        case "$action" in legacy|external)
            ((found == 0)) || die 'Duplicate exact-kernel entry in driver manifest.'
            found=1; kind=$action; selected=$path;;
        esac
    done < "$bundle/targets.tsv"
    if ((found)); then
        installer=$bundle/$selected/install.sh
        if [[ $kind == legacy ]]; then
            legacy_root=/var/lib/e610-ixgbe/manual-legacy-$ID-$kver
            mkdir -p "$legacy_root"
            unpack=$(mktemp -d "$legacy_root/payload.XXXXXXXX")
            expanded=$("$python" "$here/e610-extract.py" "$bundle/$selected" "$unpack")
            installer=$expanded/install.sh
        fi
        rc=0
        bash "$installer" --kernel "$kver" --check || rc=$?
        if ((rc == 3)) && [[ ! -t 0 ]]; then
            echo 'E610_MOK_PENDING: rerun from a console to enroll the signing certificate.'; exit 3
        fi
        ((rc == 0 || rc == 3)) || die "Driver preflight failed ($rc)."
        bash "$installer" --kernel "$kver"
    else
        # Exact-ABI fallback for other Linux versions. Never substitute a newer
        # kernel or force-load a different distribution's prebuilt module.
        root=/var/lib/e610-ixgbe/manual-$ID-$kver
        mkdir -p "$root"
        payload=$root/payload
        if [[ ! -f $payload/SHA256SUMS ]]; then
            [[ ${VC_MANUAL_OFFLINE:-0} == 0 || -f /lib/modules/$kver/build/Makefile ]] || die "Offline headers missing for $kver; supply matching headers/build tools."
            if [[ ${VC_MANUAL_OFFLINE:-0} == 0 ]]; then
                case "$ID" in
                    ubuntu|debian)
                        apt-get -o Acquire::Retries=1 -o Acquire::http::Timeout=20 update
                        DEBIAN_FRONTEND=noninteractive apt-get -y --no-upgrade --no-install-recommends install build-essential "linux-headers-$kver" libelf-dev openssl initramfs-tools kmod
                        ;;
                    *)
                        manager=yum; command -v dnf >/dev/null && manager=dnf
                        "$manager" -y install "kernel-devel-$kver" gcc make elfutils-libelf-devel openssl dracut kmod
                        ;;
                esac
            fi
            [[ -f /lib/modules/$kver/build/Makefile ]] || die "Exact headers for $kver unavailable; current kernel retained."
            source=$bundle/source/ixgbe-6.4.5.tar.gz
            echo "8d23f2c329e0b93055236e67ffbf1de99d8dce32ccc75beb759774c7c24d7ed4  $source" | sha256sum --status -c - || die 'Intel source checksum mismatch.'
            work=$(mktemp -d "$root/build.XXXXXXXX")
            "$python" "$here/e610-extract.py" "$source" "$work" >/dev/null
            make -C "$work/ixgbe-6.4.5/src" -j2 BUILD_KERNEL="$kver" KSRC="/lib/modules/$kver/build" > "$root/build.log" 2>&1 || die "Build failed; inspect $root/build.log. No installed module changed."
            module=$work/ixgbe-6.4.5/src/ixgbe.ko
            [[ $(modinfo -F vermagic "$module" | awk '{print $1}') == "$kver" ]] || die 'Built module ABI mismatch.'
            aliases=$(modinfo -F alias "$module")
            for id in 57AE 57AF 57B0 57B1 57B2; do [[ $aliases == *v00008086d0000$id* ]] || die "Built module lacks E610 PCI ID $id."; done
            stage=$(mktemp -d "$root/payload.XXXXXXXX")
            cp "$module" "$stage/ixgbe.ko"
            # Keys stay locally in the root-only state directory, never in the
            # downloadable package. Signed modules also work with Secure Boot off.
            if [[ ! -f $root/MOK.key ]]; then
                cat > "$root/signing.cnf" <<'CERT'
[req]
distinguished_name=dn
x509_extensions=ext
prompt=no
[dn]
CN=Local E610 manual driver
[ext]
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid
extendedKeyUsage=codeSigning,1.3.6.1.4.1.2312.16.1.2
CERT
                openssl req -new -x509 -newkey rsa:3072 -sha256 -config "$root/signing.cnf" -keyout "$root/MOK.key" -outform DER -out "$root/MOK.der" -nodes -days 3650 >/dev/null 2>&1
            fi
            /lib/modules/"$kver"/build/scripts/sign-file sha256 "$root/MOK.key" "$root/MOK.der" "$stage/ixgbe.ko"
            cp "$root/MOK.der" "$stage/E610-MOK.der"
            cp "$here/e610-mok-status.sh" "$stage/mok-status.sh"
            sed -e "s/@KERNEL@/$kver/g" -e "s/@OS@/$ID/g" -e "s/@VERSION@/$version/g" \
                -e "s/@LABEL@/$ID $VERSION_ID/g" \
                -e "s/@MODULE_SHA256@/$(sha256sum "$stage/ixgbe.ko" | awk '{print $1}')/g" \
                -e "s/@CERT_SHA256@/$(sha256sum "$stage/E610-MOK.der" | awk '{print $1}')/g" \
                "$here/e610-install-template.sh" > "$stage/install.sh"
            case "$ID" in
                ubuntu|debian)
                    printf '#!/bin/sh\ncase "${1:-}" in prereqs) exit 0;; esac\n[ "$version" = "%s" ] || exit 0\n. /usr/share/initramfs-tools/hook-functions\nmanual_add_modules ixgbe\n' "$kver" > "$stage/initramfs-hook";;
                *) printf 'add_drivers+=" ixgbe "\n' > "$stage/initramfs-hook";;
            esac
            (cd "$stage" && sha256sum ixgbe.ko E610-MOK.der mok-status.sh install.sh initramfs-hook > SHA256SUMS)
            [[ ! -e $payload ]] || die 'Incomplete prior payload exists; review it before retrying.'
            mv -T "$stage" "$payload"
        fi
        rc=0; bash "$payload/install.sh" --check || rc=$?
        if ((rc == 3)) && [[ ! -t 0 ]]; then echo 'E610_MOK_PENDING: console certificate enrollment required.'; exit 3; fi
        ((rc == 0 || rc == 3)) || die "Local driver preflight failed ($rc)."
        bash "$payload/install.sh"
    fi
fi
if ! native_support; then echo 'E610_MOK_PENDING: certificate enrollment required; no effective driver installed yet.'; exit 3; fi
# Loading an absent driver is safe; never unload/rebind a driver carrying SSH.
[[ -d /sys/module/ixgbe ]] || modprobe ixgbe
udevadm settle --timeout=10
verify
