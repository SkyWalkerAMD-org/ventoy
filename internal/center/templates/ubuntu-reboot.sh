#!/bin/sh
# Run after successful installer completion hooks, in the live installer.
set -eu
umask 077
if [ ! -f /run/.casper-boot ]; then exit 0; fi
. /etc/os-release
if [ "${ID:-}" != ubuntu ]; then exit 0; fi
if [ "$(findmnt -n -r -o FSTYPE -M /)" != overlay ]; then exit 0; fi
if [ "$(findmnt -n -r -o FSTYPE -M /cdrom)" != iso9660 ]; then exit 0; fi
case ",$(findmnt -n -r -o OPTIONS -M /cdrom)," in
    *,ro,*) ;;
    *) exit 0 ;;
esac

# Casper masks the vendor unit to avoid a coldplug race. The mount still exists
# and systemd tries to unmount it at shutdown, but masked units ignore drop-ins.
# After installation (and coldplug), a runtime unit can describe that existing
# read-only mount without removing the vendor mask or changing the target OS.
if [ "$(systemctl show cdrom.mount -p LoadState --value)" = masked ]; then
    fragment=$(systemctl show cdrom.mount -p FragmentPath --value)
    case "$fragment" in
        /lib/systemd/system/cdrom.mount|/usr/lib/systemd/system/cdrom.mount) ;;
        *) echo 'Ventoy Center: refusing to override a non-Casper mount mask.' >&2; exit 1 ;;
    esac
    test "$(readlink "$fragment")" = /dev/null
    device=$(findmnt -n -r -o SOURCE -M /cdrom)
    case "$device" in
        /dev/*) ;;
        *) echo 'Ventoy Center: live ISO is not a block device.' >&2; exit 1 ;;
    esac
    case "$device" in
        *[!a-zA-Z0-9_./:-]*|*/../*|*/./*) echo 'Ventoy Center: unsupported live ISO device path.' >&2; exit 1 ;;
    esac
    unit=/run/systemd/system/cdrom.mount
    test ! -L "$unit" && test ! -e "$unit"
    temporary=$(mktemp /run/systemd/system/.ventoy-cdrom.XXXXXXXX)
    trap 'rm -f "$temporary"' EXIT
    printf '# Ventoy Center live installer only\n[Mount]\nWhat=%s\nWhere=/cdrom\nType=iso9660\nOptions=ro\n' "$device" > "$temporary"
    chmod 0644 "$temporary"
    ln "$temporary" "$unit"
    rm -f "$temporary"
    trap - EXIT
fi

# Casper's live root uses squashfs loops backed by this read-only ISO. A normal
# unmount therefore reports EBUSY at shutdown. casper-stop can then eject the
# still-referenced device and cause squashfs I/O errors. Detach the mount at the
# normal umount.target stage, keeping the underlying references alive until
# live-root teardown. Do not unmount/eject now, force a reboot, or change /target.
# /run is intentional: this policy must never persist in the installed system.
directory=/run/systemd/system/cdrom.mount.d
test ! -L "$directory"
mkdir -p "$directory"
file="$directory/90-ventoy-center-reboot.conf"
test ! -L "$file"
temporary=$(mktemp "$directory/.ventoy-reboot.XXXXXXXX")
trap 'rm -f "$temporary"' EXIT
printf '[Mount]\nLazyUnmount=yes\n' > "$temporary"
chmod 0644 "$temporary"
mv -f "$temporary" "$file"
systemctl daemon-reload
test "$(systemctl show cdrom.mount -p LoadState --value)" = loaded
test "$(systemctl show cdrom.mount -p LazyUnmount --value)" = yes

# systemd 259 dlopens libmount; finalrd 9 only copies ldd dependencies, so its
# shutdown root lacks libmount and cannot enumerate mounts or swaps. Use the
# documented runtime hook, including libmount's dependencies, rather than
# replacing systemd, forcing a reboot or suppressing its errors.
if [ -x /usr/bin/finalrd ] && [ -r /usr/share/initramfs-tools/hook-functions ]; then
    directory=/run/finalrd
    test ! -L "$directory"
    mkdir -p "$directory"
    file=$directory/ventoy-libmount.finalrd
    test ! -L "$file"
    temporary=$(mktemp "$directory/.ventoy-finalrd.XXXXXXXX")
    trap 'rm -f "$temporary"' EXIT
    cat > "$temporary" <<'VC_FINALRD'
#!/bin/sh
set -eu
[ "${1:-}" = setup ] || exit 0
[ "${DESTDIR:-}" = /run/initramfs ] || exit 1
export verbose=n
. /usr/share/initramfs-tools/hook-functions
library=$(ldconfig -p | awk '$1=="libmount.so.1" {print $NF;exit}')
test -n "$library" && test -r "$library"
copy_exec "$library"
VC_FINALRD
    chmod 0755 "$temporary"
    mv -f "$temporary" "$file"
    trap - EXIT
fi
# Subiquity already creates this in noninteractive mode. Interactive installers
# still wait for the user's Reboot action, but need no physical media removal.
touch /run/casper-no-prompt
echo 'Ventoy Center: live ISO shutdown prepared for normal installer reboot.'
