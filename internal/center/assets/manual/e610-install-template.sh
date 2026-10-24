#!/bin/bash
# SPDX-License-Identifier: GPL-2.0-only
set -Eeuo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C
umask 077
ulimit -c 0
package_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
kver=$(uname -r)
mode=install
while (($#)); do
    case "$1" in
        --check) mode=check ;;
        --rollback) mode=rollback ;;
        --kernel) (($# >= 2)) || exit 2; kver=$2; shift ;;
        -h|--help)
            echo '用法：sudo bash install.sh [--check | --rollback] [--kernel @KERNEL@]'
            echo '首次开启 Secure Boot 时申请证书注册；重启注册后再次运行同一命令安装。'
            exit 0 ;;
        *) echo "未知参数：$1" >&2; exit 2 ;;
    esac
    shift
done
die() { echo "错误：$*" >&2; exit 1; }
[[ $kver == @KERNEL@ && $(uname -m) == x86_64 ]] || die '仅适用于 @KERNEL@ / x86_64。'
. /etc/os-release
os_version=$VERSION_ID
[[ $ID != rocky ]] || os_version=${VERSION_ID%%.*}
[[ $ID == @OS@ && $os_version == @VERSION@ ]] || die '仅适用于 @LABEL@。'
[[ $EUID == 0 ]] || die '请使用 sudo bash install.sh。'
image_tools='mkinitramfs unmkinitramfs lsinitramfs'
[[ $ID == ubuntu || $ID == debian ]] || image_tools='dracut lsinitrd'
for command in sha256sum modinfo modprobe depmod $image_tools flock find awk od tr; do
    command -v "$command" >/dev/null || die "缺少系统命令：$command，请保留报错并联系提供者。"
done
state=/var/lib/e610-ixgbe/expanded-@OS@-$kver
dest=/lib/modules/$kver/updates/e610/ixgbe.ko
hook=/etc/initramfs-tools/hooks/e610-ixgbe-@KERNEL@
initrd=/boot/initrd.img-$kver
if [[ $ID != ubuntu && $ID != debian ]]; then
    hook=/etc/dracut.conf.d/90-e610-$kver.conf
    initrd=/boot/initramfs-$kver.img
fi
backup=
temp_image=
temp_module=
extract_dir=
changed=0
cleanup() {
    [[ -z $temp_image ]] || rm -f -- "$temp_image"
    [[ -z $temp_module ]] || rm -f -- "$temp_module"
    [[ -z $extract_dir ]] || rm -rf -- "$extract_dir"
    return 0
}
trap cleanup EXIT
status() { printf '%s\n' "$1" > "$backup/status.new" && mv -f "$backup/status.new" "$backup/status"; }
get_backup() {
    local name
    name=$(readlink "$state/latest") || return 1
    [[ $name =~ ^backup\.[[:alnum:]]+$ ]] || return 1
    backup=$state/$name
    [[ -d $backup && ! -L $backup ]]
}
restore() {
    local object path original
    (cd "$backup" && sha256sum --status -c backup.sha256) || return 1
    for object in module hook; do
        if [[ $object == module ]]; then path=$dest; else path=$hook; fi
        original=$(cat "$backup/$object-state") || return 1
        case "$original" in
            present)
                temp_module=$(mktemp "${path%/*}/.e610-restore.XXXXXXXX") || return 1
                cp -a "$backup/previous-$object" "$temp_module" || return 1
                mv -f "$temp_module" "$path" || return 1
                temp_module= ;;
            absent) rm -f "$path" || return 1 ;;
            *) return 1 ;;
        esac
    done
    if [[ ! -f $initrd ]] || ! cmp -s "$backup/initrd" "$initrd"; then
        temp_image=$(mktemp "/boot/.e610-restore.XXXXXXXX") || return 1
        cp -a "$backup/initrd" "$temp_image" || return 1
        mv -f "$temp_image" "$initrd" || return 1
        temp_image=
    fi
    depmod -a "$kver" || return 1
    status rolled-back || return 1
}
failed() {
    local rc=$1
    trap - ERR INT TERM
    ((rc != 0)) || rc=1
    if ((changed)); then
        echo '安装失败，正在恢复原模块和启动镜像……' >&2
        if restore; then echo '已恢复安装前状态。' >&2
        else echo "恢复未完成，请保留 $backup 并从控制台处理。" >&2; fi
    fi
    exit "$rc"
}
trap 'failed $?' ERR
trap 'failed 130' INT
trap 'failed 143' TERM
exec 9>/var/lock/e610-ixgbe.lock
flock -n 9 || die '另一个安装进程正在运行。'
if [[ $mode == rollback ]]; then
    get_backup || die '没有可用的安装前备份。'
    [[ $(cat "$backup/status") != rolled-back ]] || { echo '已经回退。'; exit 0; }
    restore || die '恢复失败，请保留备份并从控制台处理。'
    echo '已恢复原模块和启动镜像。请在维护窗口重启。MOK 公钥保留，不自动更改固件信任。'
    exit 0
fi
(cd "$package_dir" && sha256sum --status -c SHA256SUMS) || die '安装包校验失败，请重新解压。'
expected_module='@MODULE_SHA256@'
expected_cert='@CERT_SHA256@'
[[ $(sha256sum "$package_dir/ixgbe.ko" | awk '{print $1}') == "$expected_module" ]] || die '驱动校验失败。'
[[ $(sha256sum "$package_dir/E610-MOK.der" | awk '{print $1}') == "$expected_cert" ]] || die '证书校验失败。'
[[ $(modinfo -F vermagic "$package_dir/ixgbe.ko" | awk '{print $1}') == "$kver" ]] || die '驱动内核版本不匹配。'
[[ $(modinfo -F version "$package_dir/ixgbe.ko") == 6.4.5 ]] || die '驱动版本不匹配。'
[[ -n $(modinfo -F signer "$package_dir/ixgbe.ko") ]] || die '模块缺少签名。'
[[ -d /lib/modules/$kver && -f $initrd && ! -L $initrd ]] || die '缺少目标内核或正常的启动镜像。'
for path in "$dest" "$hook"; do
    [[ ! -L $path && ( ! -e $path || -f $path ) ]] || die "目标路径不是常规文件：$path"
done
[[ ! -e $dest.zst && ! -e $dest.xz && ! -L $state ]] || die '发现冲突文件或异常备份目录。'
# Read firmware directly: mokutil --sb-state may crash on some firmware.
secure=0
if [[ -d /sys/firmware/efi ]]; then
    secure=unknown
    for variable in /sys/firmware/efi/efivars/SecureBoot-*; do
        [[ -r $variable ]] || continue
        # Read from a pipe: some efivarfs/kernel versions do not honour lseek.
        value=$(cat "$variable" | od -An -j4 -N1 -tu1 | tr -d '[:space:]')
        case "$value" in 0|1) secure=$value ;; esac
    done
fi
if grep -qsE '\[integrity\]|\[confidentiality\]' /sys/kernel/security/lockdown ||
   grep -qisE '^(1|y|yes)$' /sys/module/module/parameters/sig_enforce 2>/dev/null ||
   grep -qE '(^| )module.sig_enforce=(1|y|Y|true)( |$)' /proc/cmdline; then secure=1; fi
if [[ $secure != 0 ]]; then
    command -v mokutil >/dev/null || die '需要 mokutil 注册证书；请保留报错。'
    . "$package_dir/mok-status.sh"
    result=0
    e610_mok_status "$package_dir/E610-MOK.der" || result=$?
    if ((result != 0)); then
        [[ $result == 1 || $result == 2 ]] || die '无法确定证书注册状态，请保留上面的输出后重试。'
        echo '首次使用：需要在 MOK 界面注册本包的公钥，驱动尚未安装。'
        echo "证书 SHA-256：$expected_cert"
        [[ $mode != check ]] || exit 3
        if [[ $result == 1 ]]; then
            echo '请设置一次性注册密码；重启时 Enroll MOK → Continue → Yes → 输入密码 → Reboot。'
            mokutil --import "$package_dir/E610-MOK.der"
        fi
        echo '注册申请已提交。请重启完成注册，再次执行 sudo bash install.sh。'
        echo 'E610_MOK_REBOOT_REQUIRED'
        exit 0
    fi
fi
# Confirm that the in-tree dependencies already exist before modifying the system.
deps=$(modinfo -F depends "$package_dir/ixgbe.ko")
for dependency in ${deps//,/ }; do
    modinfo -k "$kver" "$dependency" >/dev/null || die "系统缺少依赖模块 $dependency，请保留报错以补齐离线依赖。"
done
echo "校验通过：@LABEL@ / $kver / ixgbe 6.4.5；Secure Boot 状态：$secure"
[[ $mode != check ]] || { echo '预检查通过，未安装驱动。'; exit 0; }
if [[ -e $state/latest || -L $state/latest ]]; then
    get_backup || die '备份索引异常。'
    case "$(cat "$backup/status")" in
        installed)
            if [[ -f $dest ]] && [[ $(sha256sum "$dest" | awk '{print $1}') == "$expected_module" ]] &&
               cmp -s "$package_dir/initramfs-hook" "$hook" &&
               [[ $(readlink -f "$(modinfo -k "$kver" -n ixgbe)") == $(readlink -f "$dest") ]]; then
                echo '相同驱动已安装，保留原备份。请重启后检查网卡。'; exit 0
            fi
            die '已有安装记录但文件已变化，请先 --rollback。' ;;
        rolled-back) ;;
        *) die '前次安装未完成，请先 --rollback。' ;;
    esac
fi
install -d -m 700 /var/lib/e610-ixgbe "$state"
backup=$(mktemp -d "$state/backup.XXXXXXXX")
cp -a "$initrd" "$backup/initrd"
for object in module hook; do
    if [[ $object == module ]]; then path=$dest; else path=$hook; fi
    if [[ -f $path ]]; then
        cp -a "$path" "$backup/previous-$object"
        echo present > "$backup/$object-state"
    else echo absent > "$backup/$object-state"; fi
done
(cd "$backup" && shopt -s nullglob && sha256sum initrd module-state hook-state previous-*) > "$backup/backup.sha256"
# Validate the backup immediately, including all presence markers.
(cd "$backup" && sha256sum --status -c backup.sha256)
status in-progress
ln -sfn "${backup##*/}" "$state/latest.new"
mv -Tf "$state/latest.new" "$state/latest"
echo "备份：$backup"
changed=1
install -d -m 755 "${dest%/*}" "${hook%/*}"
temp_module=$(mktemp "${dest%/*}/.e610-install.XXXXXXXX")
install -m 644 "$package_dir/ixgbe.ko" "$temp_module"
mv -f "$temp_module" "$dest"
temp_module=
install -m 755 "$package_dir/initramfs-hook" "$hook"
if command -v restorecon >/dev/null; then restorecon "$dest" "$hook"; fi
depmod -a "$kver"
[[ $(readlink -f "$(modinfo -k "$kver" -n ixgbe)") == $(readlink -f "$dest") ]]
temp_image=$(mktemp "/boot/.e610-initrd.XXXXXXXX")
extract_dir=$(mktemp -d /var/tmp/e610-initrd-check.XXXXXXXX)
if [[ $ID != ubuntu && $ID != debian ]]; then
    dracut --force --no-hostonly --nostrip --kver "$kver" --add-drivers ixgbe "$temp_image"
    (cd "$extract_dir" && lsinitrd --unpack "$temp_image")
else
    mkinitramfs -o "$temp_image" "$kver"
    lsinitramfs "$temp_image" | grep -F "updates/e610/ixgbe.ko" >/dev/null
    unmkinitramfs "$temp_image" "$extract_dir"
fi
mapfile -t image_modules < <(find "$extract_dir" -type f -path '*/updates/e610/ixgbe.ko')
[[ ${#image_modules[@]} == 1 ]]
[[ $(sha256sum "${image_modules[0]}" | awk '{print $1}') == "$expected_module" ]]
chmod --reference="$initrd" "$temp_image"
mv -f "$temp_image" "$initrd"
temp_image=
if command -v restorecon >/dev/null; then restorecon "$initrd"; fi
status installed
changed=0
echo '安装完成。请在维护窗口重启，保持启动 @KERNEL@。'
echo '重启后执行：ip -br link；cat /sys/module/ixgbe/version'
echo '回退命令：sudo bash install.sh --rollback'
echo 'E610_OFFLINE_INSTALL_PASS'
