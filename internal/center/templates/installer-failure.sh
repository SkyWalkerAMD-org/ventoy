# Report only this installation's terminal failure. No disk operations and no
# shared menu/admin credential. Power/network loss is not treated as success.
vc_install_failed() (
    umask 077
    for vc_support in /usr/local/lib/ventoy-preload /ventoy-manual-preload /usr/local/lib/ventoy-install-support; do
        if [ -f "$vc_support/support-diagnostic.sh" ]; then
            /bin/sh "$vc_support/support-diagnostic.sh" failure 'Installer command failed; inspect original logs below' || true
            break
        fi
    done
    vc_failure_dir=$(mktemp -d /tmp/ventoy-install-failure.XXXXXX) || exit 0
    trap 'rm -rf -- "$vc_failure_dir"' EXIT
    printf '%s' {{sh .CA}} > "$vc_failure_dir/ca.crt"
    if command -v curl >/dev/null 2>&1; then
        printf 'header = "Authorization: Bearer %s"\n' {{sh .Token}} > "$vc_failure_dir/client.conf"
        curl --config "$vc_failure_dir/client.conf" {{if .CA}}--cacert "$vc_failure_dir/ca.crt"{{end}} \
            --silent --show-error --fail --connect-timeout 5 --max-time 15 \
            -H 'Content-Type: application/json' --data '{"state":"failed"}' {{sh (printf "%s/event" .Base)}} >/dev/null 2>&1 || true
    elif command -v wget >/dev/null 2>&1; then
        printf 'header = Authorization: Bearer %s\n' {{sh .Token}} > "$vc_failure_dir/client.conf"
        WGETRC="$vc_failure_dir/client.conf" wget {{if .CA}}--ca-certificate="$vc_failure_dir/ca.crt"{{end}} \
            --tries=1 --timeout=15 --header='Content-Type: application/json' \
            --post-data='{"state":"failed"}' -O /dev/null {{sh (printf "%s/event" .Base)}} >/dev/null 2>&1 || true
    fi
)
