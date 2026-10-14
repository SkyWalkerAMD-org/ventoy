# CentOS Linux 7.9 安装

使用 `centos-7.9` profile 和 x86_64 DVD / Everything 2009 镜像。当前优先完成该版本，其余发行版按 [18 个目标版本清单](linux-targets.md) 逐项推进。

## 镜像

本轮验证使用官方 DVD 2009，文件内容与 [CentOS Vault SHA-256 清单](https://vault.centos.org/7.9.2009/isos/x86_64/sha256sum.txt) 一致：

```text
官方文件名：CentOS-7-x86_64-DVD-2009.iso
现场文件名：CentOS-7.9-x86_64-DVD-2009.iso
大小：4712300544 bytes
SHA-256：e33d7b1ea7a9e2f38c8f693215dd85254c3a4fe446f93f563279715b68d07987
```

两种文件名都能建议 `centos-7.9`；文件名仅帮助选择，不能代替校验和及镜像内容验证。Everything 模板可选择，但本轮没有对 Everything ISO 做完整安装验证。Minimal、NetInstall 和 Live 不适用于当前完整 DVD 模板。

安装从 DVD 读取软件包，不依赖已停用的在线镜像列表。安装后的额外软件与更新需要单独准备可用的软件源，本模板不会暗中切换到第三方仓库。

## 7.9 专用兼容处理

- DVD 内的最小环境标识是 `minimal`，配置使用 `@^minimal` 和 `@core`；Rocky 保留其 `minimal-environment`。
- CentOS 7 安装环境没有 `base64`。安装前脚本直接写入经过 shell 转义的 PEM CA，仍然验证控制器 HTTPS 证书。
- 旧版 `lsblk` 对 VirtIO 磁盘可能不显示序列号。为空时读取该整盘的 `/sys/block/磁盘名/serial`，检查全部整盘并要求唯一匹配；不会退回“第一块盘”。
- `operator` 是系统内置账户，不能当作普通管理员创建。默认改为 `deployadmin`；首次启动核对普通用户 UID 与可登录 shell 后才回传完成状态。
- iVentoy 会在 udev 重命名网卡前加载驱动。CentOS 7 的早期 Kickstart 解析器把第一条网络指令转换为 `ip=ethN:dhcp`，可能一直等待已不存在的名字。先用只含 hostname 的网络指令，再用 `--device=MAC --bindto=mac --activate` 配置网卡，让 iVentoy 提供启动网络、Anaconda 按 MAC 接续网络。原始 DVD 解析器已在隔离环境验证，不生成错误的 `ip=` / `bootdev=` 或网络等待标记，生成的 ifcfg 保留 MAC、DHCP 和自动连接。

## iVentoy 操作

把已核验 ISO 放入 iVentoy 的 `iso/CentOS/`，在原生界面刷新镜像。普通网启验证从客户端 Boot 菜单选择实际接线网口的 **UEFI PXE IPv4**，进入 `CentOS` 目录，选择 ISO，再选择 `Install CentOS 7`。

仅验证网启时停在欢迎 / 语言页，不开始安装。若安装器报告现有磁盘 UUID 重复，应退出并单独核实存储；不要把这类存储错误当作 PXE 传输失败，也不要为通过启动检查而清盘。

正式自动安装需要独立登记 MAC、目标整盘序列号及启动模式，核对并授权对应任务，再按 [iVentoy 文件准备流程](iventoy-files.md) 生成和绑定专属镜像 / Kickstart。不要将某台机器的任务脚本设为所有镜像的公共默认配置。

### iVentoy 1.0.44 的 CentOS 7 脚本入口

现场客户端日志确认，专属 Kickstart 已传入 initramfs，内容与服务器一致，但 iVentoy 的 CentOS hook 把 `inst.ks` 追加到 `/etc/vtoycmdline`；原版 CentOS 7 dracut 未读取这个路径，安装器仍进入欢迎页。`Standard` 和 `Not Supported` 两种模式均复现，因此不能靠切换模式解决。

CentOS 7.9 安装包现在额外生成 `centos7-iventoy.tar.gz`。它通过 [iVentoy 官方文件注入机制](https://www.iventoy.com/en/doc_injection.html)，在 initramfs 中放置单个普通文件 `etc/cmdline.d/99-ventoy-center-ks.conf`，内容为 `inst.ks=file:/ventoy/autoinstall`。该路径由 dracut 的参数读取逻辑支持；不改写原 ISO、iVentoy 程序或磁盘保护脚本。

在 iVentoy 镜像管理中，对同一份专属任务镜像同时关联：

- 自动安装脚本：`kickstart.cfg`，默认编号与非零超时按原流程设置。
- 启动注入文件：`centos7-iventoy.tar.gz`，管理端“iVentoy 文件”弹窗会显示完整路径。

兼容包仅为 CentOS 7.9 任务生成，仍需通过 MAC、磁盘序列号、现有 RAID/LVM 和一次性授权检查。旧任务的文件清单不会被自动覆盖；缺少兼容包时取消并清理旧任务，重新生成。此兼容入口已在下述实机环境完成安装验收。

## 验证边界

2026-10-13，`0.3.0-alpha.2` 使用上述 DVD 和管理端实际生成的 Kickstart，在 KVM / QEMU 10.1、OVMF UEFI、2 vCPU、3 GiB RAM、24 GiB 新建虚拟磁盘上通过以下验证：

| 项目 | 结果 |
| --- | --- |
| 安装入口 | 从 DVD 提取的内核 / initrd 启动安装器，自动读取生成的 Kickstart |
| 完整安装 | 无人工应答，安装 306 个软件包、分区、引导程序及首次启动服务 |
| 磁盘启动 | 移除 ISO、直接内核及 initrd 参数后，仅从安装好的 qcow2 启动 |
| 系统版本 | CentOS Linux 7.9.2009，内核 `3.10.0-1160.el7.x86_64` |
| 管理账户 | `deployadmin`，UID 1000，属于 wheel；SSH 公钥登录与 sudo 密码验证通过 |
| 网络与安全设置 | VirtIO 网卡 DHCP，sshd active，SELinux Enforcing，root 密码锁定 |
| 首次启动回报 | `ready → installing → installed`；启动服务 Result=success，临时凭据脚本已移除 |
| 凭据重放 | 已完成任务再次领取安装，HTTP 401 拒绝 |

单元测试、安装配置解析、虚拟机安装和真实硬件 PXE 是不同层次。虚拟机验证使用独立控制器数据、新建 qcow2、用户态 NAT；不挂载宿主机物理盘，也不修改 fyhocsys / sckocstp。

### 真实硬件 PXE 完整安装

2026-10-14，`0.3.0-alpha.3` 使用同一份官方 DVD，在 Xeon w7-2495X、64 GiB 内存、Intel X710 LAN1 和两块 HFS480G3H2X069N SATA 磁盘上完成实装。服务端为 Rocky Linux 9.8，iVentoy 为 1.0.44，使用 `ProxyNet` 和 `Standard` 模式，客户端 Secure Boot 关闭。

| 项目 | 结果 |
| --- | --- |
| 网启与脚本 | 从 LAN1 的 UEFI PXE 进入专属任务镜像；Kickstart 与兼容注入包生效，自动通过安装前检查 |
| 完整安装 | 自动分区并安装 305 个软件包，写入 UEFI 引导程序，自动重启；安装器内无需人工应答 |
| 目标盘 | 按序列号选择尾号 Q0Q 的 SATA 盘；EFI、`/boot`、LVM 根目录、swap 与 `/home` 均位于此盘 |
| 其他磁盘 | 尾号 Q0R 的盘保留原有 953 MiB vfat 与 446.2 GiB RAID 成员分区，未挂载；未对其执行清除操作 |
| 硬盘启动 | `BootCurrent: 0000` 对应目标盘的 `EFI/CENTOS/SHIMX64.EFI`；根目录为本地 LVM / XFS，无网络 ISO 挂载 |
| 系统版本 | CentOS Linux 7.9.2009，内核 `3.10.0-1160.el7.x86_64` |
| 管理账户 | `deployadmin`，UID 1000、wheel；控制台密码登录、SSH 公钥登录与 sudo 密码验证均通过 |
| 网络与安全设置 | LAN1 在已安装系统中为 `eno2`，DHCP 与默认路由正常；sshd active、SELinux Enforcing、root 密码锁定，无 failed systemd 单元 |
| 首次启动回报 | `ready → installing → installed`；回报服务 Result=success / ExecMainStatus=0，服务自动禁用、临时凭据脚本已移除 |
| 凭据重放 | 用已完成任务原凭据重新请求安装授权，HTTP 401 拒绝；未为测试重放再次启动安装器 |

本轮服务端于 `04:12:17 UTC` 收到写盘前领取，`04:16:39 UTC` 收到首次启动完成回报。KVM 画面、服务端状态和 SSH 检查相互核对；测试凭据及原始日志留在现场私有验收目录，不入仓库。

**旧存储准备是独立步骤。** 测试机此前的两块盘组成 Debian RAID1，安装前保护按预期报告 `Existing LVM/RAID members require manual storage preparation`。在用户已授权清盘的前提下，核对两块整盘序列号、无挂载 / swap、阵列仅含预期成员后，停止离线阵列，仅清除目标盘的分区与 RAID 标记，再重新网启。这里验证的是已准备目标盘的单盘安装，没有增加自动拆除现有 RAID / LVM 的功能。

排障中遇到的两项启动兼容问题已分别解决：`cmdline.d` 注入使 dracut 读取 Kickstart；网络指令顺序避免等待 udev 重命名前的 `eth1`。旧 RAID 保护触发时，CentOS 7 的错误处理还可能显示 `ImportError: No module named updates_disk_hook`；应先检查 `/tmp/ventoy-center-pre.log`，本轮实际原因是旧阵列，未通过修改 Python 模块绕过保护。

验收结束时复核 iVentoy 仍为运行状态，保持 `Standard` / `ProxyNet`，无全局默认镜像。上述结果仅覆盖此 DVD、硬件和 UEFI 配置，不代表 Everything、Legacy BIOS、Secure Boot 或其他发行版已完成实装验收。
