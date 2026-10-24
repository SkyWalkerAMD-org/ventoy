# Verify the selected distribution before claiming/completing installation.
. /etc/os-release
[ "${ID:-}" = {{sh .Target.OS}} ] || { echo 'Distribution mismatch' >&2; exit 1; }
{{if eq .Target.ID "centos-7.6"}}[ "${VERSION_ID:-}" = 7 ] && grep -Eq '^CentOS Linux release 7\.6\.1810([ (]|$)' /etc/centos-release || { echo 'CentOS 7.6.1810 required' >&2; exit 1; }
{{else if eq .Target.ID "centos-8.0"}}[ "${VERSION_ID:-}" = 8 ] && grep -Eq '^CentOS Linux release 8\.0\.1905([ (]|$)' /etc/centos-release || { echo 'CentOS 8.0.1905 required' >&2; exit 1; }
{{else if eq .Target.ID "centos-8.4"}}[ "${VERSION_ID:-}" = 8 ] && grep -Eq '^CentOS Linux release 8\.4\.2105([ (]|$)' /etc/centos-release || { echo 'CentOS 8.4.2105 required' >&2; exit 1; }
{{else if eq .Target.ID "centos-7.9"}}[ "${VERSION_ID:-}" = 7 ] && grep -Eq 'release 7\.9([. ]|$)' /etc/centos-release || { echo 'CentOS 7.9 required' >&2; exit 1; }
{{else if eq .Target.ID "centos-8.5"}}[ "${VERSION_ID:-}" = 8 ] && grep -Eq '^CentOS Linux release 8\.5\.2111([ (]|$)' /etc/centos-release || { echo 'CentOS Linux 8.5.2111 required' >&2; exit 1; }
{{else if or (eq .Target.ID "centos-stream-8") (eq .Target.ID "centos-stream-9") (eq .Target.ID "centos-stream-10")}}[ "${VERSION_ID:-}" = {{sh .Target.Version}} ] && grep -Eq '^CentOS Stream release {{.Target.Version}}([ (]|$)' /etc/centos-release || { echo 'CentOS Stream version mismatch' >&2; exit 1; }
{{else}}case "${VERSION_ID:-}" in {{.Target.Version}}|{{.Target.Version}}.*) ;; *) echo 'Distribution version mismatch' >&2; exit 1;; esac
{{end}}
