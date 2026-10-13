# Verify the selected distribution before claiming/completing installation.
. /etc/os-release
[ "${ID:-}" = {{sh .Target.OS}} ] || { echo 'Distribution mismatch' >&2; exit 1; }
{{if eq .Target.OS "centos"}}[ "${VERSION_ID:-}" = 7 ] && grep -Eq 'release 7\.9([. ]|$)' /etc/centos-release || { echo 'CentOS 7.9 required' >&2; exit 1; }
{{else}}case "${VERSION_ID:-}" in {{.Target.Version}}|{{.Target.Version}}.*) ;; *) echo 'Distribution version mismatch' >&2; exit 1;; esac
{{end}}
