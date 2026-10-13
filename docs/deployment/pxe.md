# iVentoy / PXE 与外部 DHCP

客户端路径：BIOS / UEFI → Boot 菜单中的网卡 → iVentoy → 对应系统安装器。

当前先做 Linux 系统安装，版本与镜像要求见 [Linux 安装范围](linux-targets.md)。Windows 为次要支持；sckocstp / fyhocsys 接入不属于本阶段。

## 与已有 DHCP 配合

本项目不提供 DHCP 服务，也不修改 DHCP 配置。已有服务器负责分配地址。按 iVentoy 的[第三方 DHCP 官方说明](https://www.iventoy.com/cn/doc_ext_dhcp.html)选择与网段拓扑相符的模式：同网段外部 DHCP、跨网段中继与 Proxy 场景不能混用配置。

具体应填写的 `next-server`、bootfile 与客户端架构有关；应从所安装 iVentoy 版本的说明取得，不能给 BIOS、UEFI32、UEFI64 都硬写同一个引导文件。启用前先核对 DHCP 服务器产品、作用域、iVentoy IP 和客户端是否跨 VLAN。不要在已有 DHCP 的网络上直接启用第二个地址池。

iVentoy 的当前[端口说明](https://www.iventoy.com/cn/doc_portnum.html)包含 UDP 67/68、69、4011（按模式），TCP 26000（管理）、16000（启动 HTTP）、10809（NBD）、3260（iSCSI）、12049（NFS）、10445（SMB）。以实际启用的模式和版本为准，只向需要的网段开放。新管理端的 TCP 26080 和 fyhocsys 的 TCP 443 单独配置。

## 每台机器的安装包

1. 在本项目登记机器。Linux 可通过 `ip link`、`lsblk -dn -o NAME,SERIAL,TRAN` 核对信息；Windows PE 通过 `Get-CimInstance Win32_NetworkAdapter` 和 `Get-Disk` 核对。
2. 创建任务并选择系统版本与源 ISO。安装目标限定为单块、序列号可识别的非 USB 磁盘；已有 RAID / LVM 成员会被拒绝，需先人工准备存储。
3. 授权后，Linux 可用“iVentoy 文件”直接[准备同机文件](iventoy-files.md)，随后按第 5 步关联。也可下载 ZIP，包含任务清单、自动配置、注入文件和校验清单。
4. 解压到 iVentoy 的 `user/managed/任务号`。为此任务建立独立 ISO 路径，可使用同文件系统硬链接。
5. 在 iVentoy 镜像管理页把此 ISO 关联到该任务的自动脚本。设置脚本默认编号与非零超时，详见[全自动安装说明](https://www.iventoy.com/cn/doc_unattend_install.html)。
6. 在专业版[分机独启](https://www.iventoy.com/cn/doc_matrix_boot.html)中，将准确 MAC 关联到该 ISO 路径。未知机器保留普通菜单，不使用会清盘的全局默认配置。
7. 目标机器使用一次性网卡启动。安装前会向管理端领取单次授权；失败、过期、取消或重复领取均阻止自动写盘。

同一个 ISO 的全局自动脚本不能同时承载多台机器的不同配置。alpha 版本通过独立 ISO 路径隔离每台任务；未来的自动配置适配器仍需保持这一约束。

## Windows 特别准备

Windows 11 / Server 2022 的 WinPE 必须预装匹配架构和 ADK 版本的 WinPE-WMI、WinPE-NetFX、WinPE-Scripting、WinPE-PowerShell、WinPE-StorageWMI，并加入目标网卡和存储驱动。参考 [Microsoft WinPE 可选组件说明](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/winpe-add-packages--optional-components-reference?view=windows-11)。这些组件来自用户持有的 ADK，不在仓库或交付包里。

运行安装包内 `Make-Injection.ps1 -SevenZip '完整路径\7z.exe'`，把生成的 `injection.7z` 在 iVentoy 中作为该 ISO 的注入文件，同时绑定 `autounattend.xml`。缺少保护脚本或所需组件时会停在保护环节。

Windows 用显式磁盘编号和序列号双重核对后分区，UEFI 使用 GPT（EFI / MSR / 系统），BIOS 使用 MBR（引导 / 系统）。当前是最小布局，尚未自动部署专用 WinRE 恢复分区。Windows 11 应使用符合官方要求的 UEFI 硬件，不跳过 TPM / Secure Boot 要求。

安装器完成镜像应用、进入 specialize 阶段后回传“系统已安装”；OOBE 完成和驱动健康仍需验收。Windows fyhocsys 节点目前未实现。

## 完成后

删除本次 MAC 规则、ISO 别名和任务安装文件，恢复客户端正常磁盘启动。即使遗留旧安装包，管理端也拒绝重复领取安装授权；这不能代替现场的启动顺序维护。
