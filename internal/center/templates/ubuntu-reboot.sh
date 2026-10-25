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
test "$(systemctl show cdrom.mount -p LazyUnmount --value)" = yes
# Subiquity already creates this in noninteractive mode. Interactive installers
# still wait for the user's Reboot action, but need no physical media removal.
touch /run/casper-no-prompt
echo 'Ventoy Center: live ISO shutdown prepared for normal installer reboot.'
