#!/bin/sh
set -eu
# iVentoy invokes this entry after selecting and expanding the install script.
# A missing or different script leaves the ordinary confirmation in place.
[ -f /ventoy/autoinstall ] || exit 0
# iVentoy has not created /dev at this stage; keep checksum output in its log.
printf '%s\n' '{{.UserDataSHA512}}  /ventoy/autoinstall' | sha512sum -c - || exit 1
[ -f /scripts/casper ] || exit 0
[ "$(grep -c '^[[:space:]]*maybe_break init$' /init)" = 1 ] || exit 1
sed -i '/^[[:space:]]*maybe_break init$/i /ventoy/busybox/sh /ventoy-center/ubuntu-live.sh "$rootmnt"' /init
