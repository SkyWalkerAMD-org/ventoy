# Explicit fixed filesystems; only /home consumes the remaining capacity.
cat > /tmp/ventoy-center-partition.ks <<VC_SINGLE_KS
ignoredisk --only-use=$disk
clearpart --all --initlabel --disklabel=gpt --drives=$disk
bootloader --boot-drive=$disk
{{if eq .Machine.Firmware "uefi"}}part /boot/efi --fstype=efi --size=1024 --ondisk=$disk
{{else}}part biosboot --fstype=biosboot --size=1 --ondisk=$disk
{{end}}part /boot --fstype=ext4 --size=2048 --ondisk=$disk
part /tmp --fstype={{.DataFilesystem}} --size=$vc_tmp_mib --ondisk=$disk
part swap --fstype=swap --size=$vc_swap_mib --ondisk=$disk
part /var --fstype={{.DataFilesystem}} --size=$vc_var_mib --ondisk=$disk
part / --fstype={{.DataFilesystem}} --size=$vc_root_mib --ondisk=$disk
part /home --fstype={{.DataFilesystem}} --size=8192 --grow --ondisk=$disk
VC_SINGLE_KS
