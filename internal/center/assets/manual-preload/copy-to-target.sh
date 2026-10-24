#!/bin/sh
# Installer completion hook: stage the package, never execute it.
set -eu
src=${0%/*}
[ "$src" != "$0" ] || src=.
src=$(CDPATH= cd -- "$src" && pwd)
target=${1:-${TARGET_MOUNT_POINT:-/target}}
[ "$target" != / ] && [ -d "$target" ] && [ -f "$target/etc/os-release" ] || {
    echo 'Ventoy preload: installed target root is missing' >&2; exit 1;
}
target=$(readlink -f "$target")
[ "$target" != / ] || exit 1
# No symlink in the destination may redirect a write outside the target.
for dir in "$target/root" "$target/var" "$target/var/lib" "$target/var/lib/ventoy-center"; do
    [ ! -L "$dir" ] || { echo "Ventoy preload: unsafe directory $dir" >&2; exit 1; }
done
cd "$src"
sha256sum -c setup.sha256 || exit 1
mkdir -p "$target/root" "$target/var/lib/ventoy-center"
dest=$target/root/ventoy-linux-setup.run
if [ -e "$dest" ] || [ -L "$dest" ]; then
    [ -f "$dest" ] && [ ! -L "$dest" ] && cmp -s ventoy-linux-setup.run "$dest" || {
        echo 'Ventoy preload: existing setup differs; retained' >&2; exit 1;
    }
else
    tmp=$(mktemp "$target/root/.ventoy-setup.XXXXXX")
    trap 'rm -f "$tmp"' EXIT HUP INT TERM
    cp ventoy-linux-setup.run "$tmp"
    chmod 0700 "$tmp"
    # Publish without overwriting an existing file, including a concurrent one.
    ln "$tmp" "$dest"
    rm -f "$tmp"
    trap - EXIT HUP INT TERM
fi
[ "$(sha256sum "$dest" | cut -d ' ' -f 1)" = "$(cut -d ' ' -f 1 setup.sha256)" ]
for name in README-ventoy-setup.txt ventoy-linux-setup.run.sha256; do
    [ ! -e "$target/root/$name" ] && [ ! -L "$target/root/$name" ] || continue
    case "$name" in
        README*) cp README.txt "$target/root/$name" ;;
        *) cp setup.sha256 "$target/root/$name" ;;
    esac
    chmod 0600 "$target/root/$name"
done
status=$target/var/lib/ventoy-center/manual-setup-preloaded
[ ! -L "$status" ] || exit 1
printf '%s\n' 'PRELOADED_ONLY' "sha256=$(cut -d ' ' -f 1 setup.sha256)" \
    'run=sudo bash /root/ventoy-linux-setup.run --apply' > "$status"
chmod 0600 "$status"
echo 'Ventoy setup preloaded in /root/ventoy-linux-setup.run; configuration has NOT been applied.'
