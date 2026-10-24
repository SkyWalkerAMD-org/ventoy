#!/bin/sh
set -eu
# Preserve snap's own environment, mount namespace and Python interpreter.
# The 22.04.3 and 24.04.3 snap launchers discard arguments; call the Python module
# through snap's shell entry point instead. No installed snap file is edited.
exec /usr/bin/snap run --shell {{if .Target.Desktop}}ubuntu-desktop-bootstrap{{else}}subiquity{{end}}.subiquity-server -c '
set -eu
{{if .Target.Desktop}}export PYTHON="$SNAP/usr/bin/python3.12"
export PYTHONPATH="$SNAP/usr/lib/python3/site-packages:$SNAP/usr/lib/python3/dist-packages:$SNAP/lib/python3.12/site-packages"
export PY3OR2_PYTHON="$PYTHON"
export PYTHONIOENCODING=utf-8
export SUBIQUITY_ROOT="$SNAP/bin/subiquity"
export PATH="$SNAP/bin:$SNAP/sbin:$PATH"
cd "$SUBIQUITY_ROOT"
exec "$PYTHON" -m subiquity.cmd.server --storage-version=2 --postinst-hooks-dir="$SNAP/etc/subiquity/postinst.d" --no-wlan-listener --kernel-cmdline "$(cat /proc/cmdline) autoinstall" --autoinstall /autoinstall.yaml
{{else}}for site in "$SNAP"/lib/python*/site-packages "$SNAP"/usr/lib/python3*/dist-packages; do
    [ -d "$site/subiquity" ] || continue
    export PYTHONPATH="${PYTHONPATH:+$PYTHONPATH:}$site"
    exec "$PYTHON" -m subiquity.cmd.server --kernel-cmdline "$(cat /proc/cmdline) autoinstall"
done
echo "Ventoy Center: unsupported Subiquity snap layout" >&2
exit 1
{{end}}
'
