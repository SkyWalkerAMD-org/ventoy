# Partman sizes are decimal MB. Preserve the requested binary allocations.
vc_efi_mb=$(vc_mib_to_mb 1024)
vc_boot_mb=$(vc_mib_to_mb 2048)
vc_tmp_mb=$(vc_mib_to_mb "$vc_tmp_mib")
vc_swap_mb=$(vc_mib_to_mb "$vc_swap_mib")
vc_var_mb=$(vc_mib_to_mb "$vc_var_mib")
vc_root_mb=$(vc_mib_to_mb "$vc_root_mib")
cat > /tmp/ventoy-center-layout.cfg <<VC_SINGLE_PRESEED
d-i partman-partitioning/default_label string gpt
d-i partman-partitioning/choose_label string gpt
d-i partman-auto/method string regular
d-i partman-auto/choose_recipe select vc-sized
d-i partman-auto/expert_recipe string vc-sized :: {{if eq .Machine.Firmware "uefi"}}$vc_efi_mb $vc_efi_mb $vc_efi_mb free \$primary{ } method{ efi } format{ }{{else}}1 1 1 free \$primary{ } method{ biosgrub }{{end}} . $vc_boot_mb $vc_boot_mb $vc_boot_mb ext4 method{ format } format{ } use_filesystem{ } filesystem{ ext4 } mountpoint{ /boot } . $vc_tmp_mb $vc_tmp_mb $vc_tmp_mb ext4 method{ format } format{ } use_filesystem{ } filesystem{ ext4 } mountpoint{ /tmp } . $vc_swap_mb $vc_swap_mb $vc_swap_mb linux-swap method{ swap } format{ } . $vc_var_mb $vc_var_mb $vc_var_mb ext4 method{ format } format{ } use_filesystem{ } filesystem{ ext4 } mountpoint{ /var } . $vc_root_mb $vc_root_mb $vc_root_mb ext4 method{ format } format{ } use_filesystem{ } filesystem{ ext4 } mountpoint{ / } . 8590 10000 -1 ext4 method{ format } format{ } use_filesystem{ } filesystem{ ext4 } mountpoint{ /home } .
VC_SINGLE_PRESEED
{{if eq .Target.Installer "ubiquity"}}# The guarded launcher loads this file only after its claim succeeds.
cat /tmp/ventoy-center-layout.cfg >> /tmp/ventoy-center-selected.cfg
{{else}}debconf-set-selections /tmp/ventoy-center-layout.cfg
{{end}}
