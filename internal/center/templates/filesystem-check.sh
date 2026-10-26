# Verify actual mounted filesystems before reporting a successful installation.
vc_check_fs() {
    vc_actual_fs=$(findmnt -rn -o TARGET,FSTYPE | awk -v target="$1" '$1 == target {print $2}')
    if [ "$vc_actual_fs" != "$2" ]; then
        printf 'Filesystem verification failed: %s expected=%s actual=%s\n' "$1" "$2" "$vc_actual_fs" >&2
        return 1
    fi
}
for vc_mount in / /tmp /var /home; do
    vc_check_fs "$vc_mount" {{sh .DataFilesystem}}
done
vc_check_fs /boot ext4
{{if eq .Machine.Firmware "uefi"}}vc_check_fs /boot/efi vfat
{{end}}printf 'FILESYSTEM_VERIFIED: / /tmp /var /home = %s; boot = ext4\n' {{sh .DataFilesystem}}
