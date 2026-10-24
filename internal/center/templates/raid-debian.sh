# Native partman recipe, applied only after the per-job claim succeeds.
case "$disk" in *[0-9]) first=/dev/${disk}p;; *) first=/dev/$disk;; esac
case "$disk2" in *[0-9]) second=/dev/${disk2}p;; *) second=/dev/$disk2;; esac
cat > /tmp/ventoy-center-raid.cfg <<VC_RAID_PRESEED
d-i partman-auto/disk string /dev/$disk /dev/$disk2
d-i grub-installer/bootdev string /dev/$disk /dev/$disk2
d-i partman-partitioning/default_label string gpt
d-i partman-partitioning/choose_label string gpt
d-i partman-auto/method string raid
d-i partman-auto/choose_recipe select vc-raid
d-i partman-auto-lvm/new_vg_name string vcvg
d-i partman-auto-lvm/guided_size string max
d-i partman-lvm/device_remove_lvm boolean true
d-i partman-md/device_remove_md boolean true
d-i partman-lvm/confirm boolean true
d-i partman-lvm/confirm_nooverwrite boolean true
d-i partman-md/confirm boolean true
d-i partman-md/confirm_nooverwrite boolean true
d-i partman-auto-raid/recipe string 1 2 0 ext4 /boot ${first}2#${second}2 . 1 2 0 lvm - ${first}3#${second}3 .
d-i partman-auto/expert_recipe string vc-raid :: {{if eq .Machine.Firmware "uefi"}}1024 1024 1024 free \$lvmignore{ } \$primary{ } method{ efi } format{ }{{else}}1 1 1 free \$lvmignore{ } \$primary{ } method{ biosgrub }{{end}} . 2048 2048 2048 raid \$lvmignore{ } \$primary{ } method{ raid } . 8192 10000 -1 raid \$lvmignore{ } \$primary{ } method{ raid } . 2048 2048 2048 linux-swap \$defaultignore{ } \$lvmok{ } in_vg{ vcvg } lv_name{ swap } method{ swap } format{ } . 8192 10000 -1 ext4 \$defaultignore{ } \$lvmok{ } in_vg{ vcvg } lv_name{ root } method{ format } format{ } use_filesystem{ } filesystem{ ext4 } mountpoint{ / } .
VC_RAID_PRESEED
debconf-set-selections /tmp/ventoy-center-raid.cfg
