#!/bin/sh
# Route only known official APT mirrors through the private signed-object cache.
# No index refresh, package installation, signature bypass, or kernel changes.
set -eu
src=$(CDPATH= cd -- "${0%/*}" && pwd)
target=${1:?target root required}
[ -d "$target/etc/apt" ] || exit 0
[ -s "$src/support-cache-url" ] || exit 0
cache=$(cat "$src/support-cache-url")
[ -n "$cache" ] || exit 0
printf '%s\n' "$cache" | grep -Eq '^http://[0-9.]+:[0-9]+/installer-cache$' || exit 1
for f in "$target/etc/apt/sources.list" "$target"/etc/apt/sources.list.d/*.list "$target"/etc/apt/sources.list.d/*.sources; do
    [ -f "$f" ] || continue
    [ ! -L "$f" ] || { echo "Retained linked APT source: $f"; continue; }
    tmp=$(mktemp "${f%/*}/.ventoy-cache.XXXXXX")
    # Preserve suites, components and Signed-By fields byte-for-byte.
    sed -E \
      -e "s@https?://archive\.ubuntu\.com/ubuntu/?([ /]|$)@$cache/ubuntu\1@g" \
      -e "s@https?://security\.ubuntu\.com/ubuntu/?([ /]|$)@$cache/ubuntu-security\1@g" \
      -e "s@https?://old-releases\.ubuntu\.com/ubuntu/?([ /]|$)@$cache/ubuntu-archive\1@g" \
      -e "s@https?://deb\.debian\.org/debian-security/?([ /]|$)@$cache/debian-security\1@g" \
      -e "s@https?://security\.debian\.org/debian-security/?([ /]|$)@$cache/debian-security\1@g" \
      -e "s@https?://deb\.debian\.org/debian/?([ /]|$)@$cache/debian\1@g" \
      -e "s@https?://archive\.debian\.org/debian/?([ /]|$)@$cache/debian-archive\1@g" "$f" > "$tmp"
    if ! cmp -s "$f" "$tmp"; then
        [ -e "$f.before-ventoy-cache" ] || cp -p "$f" "$f.before-ventoy-cache"
        chmod 0644 "$tmp"
        mv -f "$tmp" "$f"
    else rm -f "$tmp"; fi
done
mkdir -p "$target/etc/apt/apt.conf.d"
printf 'Acquire::Retries "10";\nAcquire::AllowInsecureRepositories "false";\n' > "$target/etc/apt/apt.conf.d/90ventoy-installer-cache"
chmod 0644 "$target/etc/apt/apt.conf.d/90ventoy-installer-cache"
