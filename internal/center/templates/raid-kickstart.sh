# Native Anaconda partition plan; only the two independently verified disks.
cat > /tmp/ventoy-center-partition.ks <<VC_RAID_KS
ignoredisk --only-use=$disk,$disk2
clearpart --all --initlabel --disklabel=gpt --drives=$disk,$disk2
bootloader --boot-drive=$disk
{{if eq .Machine.Firmware "uefi"}}part /boot/efi --fstype=efi --size=1024 --ondisk=$disk
part /boot/efi2 --fstype=efi --size=1024 --ondisk=$disk2
{{else}}part biosboot --fstype=biosboot --size=1 --ondisk=$disk
part biosboot --fstype=biosboot --size=1 --ondisk=$disk2
{{end}}part raid.11 --size=2048 --ondisk=$disk
part raid.21 --size=2048 --ondisk=$disk2
part raid.12 --size=8192 --grow --ondisk=$disk
part raid.22 --size=8192 --grow --ondisk=$disk2
raid /boot --device=boot --level=RAID1 --fstype=ext4 raid.11 raid.21
raid pv.01 --device=data --level=RAID1 raid.12 raid.22
volgroup vcvg pv.01
logvol /tmp --vgname=vcvg --name=tmp --fstype=ext4 --size=$vc_tmp_mib
logvol swap --vgname=vcvg --name=swap --fstype=swap --size=$vc_swap_mib
logvol /var --vgname=vcvg --name=var --fstype=ext4 --size=$vc_var_mib
logvol / --vgname=vcvg --name=root --fstype=ext4 --size=$vc_root_mib
logvol /home --vgname=vcvg --name=home --fstype=ext4 --size=8192 --grow
VC_RAID_KS
