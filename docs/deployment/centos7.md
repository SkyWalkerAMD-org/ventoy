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
- `operator` 是系统内置账户，不能当作普通管理员创建。早期模板使用 `deployadmin`，当前按用户选择默认使用 `admin`；首次启动核对普通用户 UID 与可登录 shell 后才回传完成状态。
- iVentoy 会在 udev 重命名网卡前加载驱动。CentOS 7 的早期 Kickstart 解析器把第一条网络指令转换为 `ip=ethN:dhcp`，可能一直等待已不存在的名字。先用只含 hostname 的网络指令，再用 `--device=MAC --bindto=mac --no-activate --onboot=yes` 保存目标网卡的 DHCP 配置。安装期间保留 iVentoy 已建立的连接；重新激活该网卡会先丢失 IPv4，使正在使用的 iSCSI 安装介质超时，继而导致 Anaconda 的 `NetworkManager.GetDevices` 超时。原版解析器及 Anaconda 网络初始化函数已验证：不生成错误的 `ip=` / `bootdev=` 或等待标记，不重激活 live 连接，目标 ifcfg 保留 MAC、DHCP 和自动连接。此处理覆盖 CentOS 7.6、7.9 与 RHEL 7 模板。

## iVentoy 操作

把已核验 ISO 放入 iVentoy 的 `iso/CentOS/`，在原生界面刷新镜像。普通网启验证从客户端 Boot 菜单选择实际接线网口的 **UEFI PXE IPv4**，进入 `CentOS` 目录，选择 ISO，再选择 `Install CentOS 7`。

仅验证网启时停在欢迎 / 语言页，不开始安装。若安装器报告现有磁盘 UUID 重复，应退出并单独核实存储；不要把这类存储错误当作 PXE 传输失败，也不要为通过启动检查而清盘。

当前可以直接选择 `ventoy-center → PXE-Auto → CentOS-7.9… → image.iso` 和自动安装脚本。一块可用内置盘按策略自动安装；多盘在控制台选择目标盘并输入 `yes`，或输入 `r`、两个盘号和 `yes` 重建双盘 RAID1 + LVM。脚本核对 MAC、磁盘序列号和存储成员后创建对应任务，不需要预先在后台登记。也保留 [iVentoy 文件准备流程](iventoy-files.md)中的专属任务方式；不能将某台机器的任务脚本设为所有镜像的公共默认配置。

### iVentoy 1.0.44 的 CentOS 7 脚本入口

现场客户端日志确认，专属 Kickstart 已传入 initramfs，内容与服务器一致，但 iVentoy 的 CentOS hook 把 `inst.ks` 追加到 `/etc/vtoycmdline`；原版 CentOS 7 dracut 未读取这个路径，安装器仍进入欢迎页。`Standard` 和 `Not Supported` 两种模式均复现，因此不能靠切换模式解决。

CentOS 7.9 安装包现在额外生成 `centos7-iventoy.tar.gz`。它通过 [iVentoy 官方文件注入机制](https://www.iventoy.com/en/doc_injection.html)，在 initramfs 中放置单个普通文件 `etc/cmdline.d/99-ventoy-center-ks.conf`，内容为 `inst.ks=file:/ventoy/autoinstall`。该路径由 dracut 的参数读取逻辑支持；不改写原 ISO、iVentoy 程序或磁盘保护脚本。

在 iVentoy 镜像管理中，对同一份专属任务镜像同时关联：

- 自动安装脚本：`kickstart.cfg`，默认编号与非零超时按原流程设置。
- 启动注入文件：`centos7-iventoy.tar.gz`，管理端“iVentoy 文件”弹窗会显示完整路径。

兼容包仅为 CentOS 7.9 任务生成，仍需通过 MAC、磁盘序列号、现有 RAID/LVM 和一次性授权检查。旧任务的文件清单不会被自动覆盖；缺少兼容包时取消并清理旧任务，重新生成。此兼容入口已在下述实机环境完成安装验收。

## 验证边界

### iVentoy 1.0.47 双盘 PXE-Auto 回归（2026-10-25）

alpha.28 在 `.235` / Intel X710 上修复并复测 NetworkManager 重激活 PXE 连接导致的 iSCSI 超时。按 `r → 1 2 → yes`，清理已授权的 Q0Q/Q0R，完整安装双盘 RAID1 + LVM，自行重启到硬盘系统并回报 installed；另一次正常重启后所有检查再次通过。实际 root/admin 密码 SSH、sudo、DHCP、主机名、CLI、两组 RAID1 及两盘 EFI 引导文件、内核锁定与禁用自动更新均通过。仍使用 `3.10.0-1160.el7.x86_64`，本轮没有强制复位、BIOS 干预或实物拔盘；首次阵列同步继续在后台。完整原因、发布记录与证据见[当前进度](../development/progress.md)。以下为此前各轮验收的历史边界。

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

### Intel E610 实机适配（2026-10-16）

带外 `192.168.1.39` 的机器使用 Intel E610（PCI `8086:57b0`）和 MR9540-8i RAID 逻辑盘。原版 CentOS 7.9 安装环境缺少该网卡的匹配驱动，虽然固件能进入 iVentoy，进入 Linux 后仍会因无法继续读取网络介质而停在 dracut 等待阶段。

本轮采用用户 [E610 项目的 r2 适配包](https://github.com/SkyWalkerAMD-org/E610/releases/tag/v6.4.5-centos7.9-r2)，保持原版 DVD 的 `3.10.0-1160.el7.x86_64` 内核。安装环境替换为匹配此内核的 ixgbe 6.4.5，并重新生成模块依赖；安装结束时在目标系统中运行适配包的离线安装器，将驱动放入 `updates/` 并重建目标 initramfs。只改安装环境中的驱动不足以保证安装后还能联网。

| 输入 / 产物 | SHA-256 |
| --- | --- |
| `E610-CentOS7.9-adaptation-r2.tar.gz` | `43b88ce14c6e6ddd41d99559b6fcd58c043fd6bc389e37113f7cee01eeeb308c` |
| 1160 内核对应的 `ixgbe.ko` | `4139a6de9813f4b686e980165e4dfb108f36df015dbe855fbaa014a750aaa341` |
| `CentOS-7.9-x86_64-E610-r2-DVD.iso` | `4ad8b7b4d50900294b79bc7a22e20baefac6e1351456e052a1f8e8bfb406d142` |

基础适配 ISO 保留原内核、软件包、卷标和 BIOS / UEFI 启动结构，加入完整适配包，内嵌介质校验通过。现场自动安装另外构建每台机器独立的 ISO，在启动菜单中直接传入 `inst.ks=cdrom:/ks.cfg`，并嵌入管理端 alpha.11 为该任务生成的 Kickstart。脚本保留发行版、MAC、目标盘、UEFI、旧存储和一次性授权检查，额外加入驱动持久化步骤；没有把清盘脚本设为全局默认配置，也没有切换 iVentoy 的全局安全启动设置。

本次用户明确授权清空 `.39` 的约 960 GB MR9540-8i 逻辑盘。安装前检查按 `ID_SERIAL_SHORT=600062b2176b0100313d5e8b96234c02` 唯一识别该整盘；硬件 RAID 向 Linux 提供单个逻辑块设备，本轮没有修改 RAID 控制器的阵列配置，也没有放开对已有 Linux 软件 RAID / LVM 成员的保护。

| 实机检查 | 结果 |
| --- | --- |
| 网启 | iVentoy 1.0.45 Pro，UEFI LAN2，E610 MAC 尾号 `86:b2`；自动加载驱动并读取网络 ISO |
| 无人值守 | 专属任务镜像启动后自动分区、安装 305 个软件包、部署驱动、应用更新策略并重启；安装器内无需人工应答 |
| 目标存储 | 授权逻辑盘上创建 200 MiB EFI、1 GiB `/boot`、LVM 根目录 / swap / home；未调整硬件 RAID 配置 |
| 本地启动 | `BootCurrent: 0003` 对应 `EFI/centos/shimx64.efi`，根目录来自本地逻辑盘 |
| 系统与驱动 | CentOS Linux 7.9.2009，内核 `3.10.0-1160.el7.x86_64`；ixgbe 6.4.5 的模块摘要与适配包一致，目标 initramfs 包含更新驱动 |
| E610 网络 | `enp47s0f1`，DHCP `192.168.1.46/23`，1 Gbps 全双工；到装机服务器 3 次 ping 均成功，SSH 公钥登录通过 |
| 账户与系统状态 | `deployadmin` UID 1000 / wheel，sudo 密码认证通过；SELinux Enforcing、sshd active、root 密码锁定，无失败服务 |
| 内核与更新策略 | `--check` 通过；真实 yum 配置读出 `kernel*` 排除项，yum-cron / dnf 自动更新单元被 mask |
| 完成回报 | `06:12:50 UTC` 领取安装，`06:19:16 UTC` 首次硬盘启动后回报 `installed`；完成服务退出 0 并自动禁用 |
| 旧授权重放 | 对完成任务再次请求安装，HTTP 401 拒绝，任务保持 `installed` |
| 第二次重启 | 正常重启后 boot ID 改变，仍从同一本地 CentOS 引导项启动；驱动、DHCP、SSH / sudo、更新策略检查再次通过，无失败服务 |

该结果只覆盖本机 E610 / MR9540-8i / UEFI 和 r2 的 1160 内核模块。当前协商速率为 1 Gbps，没有完成 10 Gbps 吞吐、长期压力、Secure Boot 或其他 E610 设备型号的验收。

**复用边界：** `CentOS-7.9-x86_64-E610-r2-DVD.iso` 是带安装环境驱动的基础镜像，本身不会替手动安装执行驱动持久化或更新策略。新机器自动安装仍需创建新的授权任务并构建对应的专属镜像；不要复用 `.39` 的任务 ISO / 凭据。本次完成后已将 `CentOS-7.9-E610-AUTO-BMC39.iso` 从公共启动菜单移除，私有验收副本保留，基础适配 ISO 仍可见。该构建流程目前是现场辅助流程，管理页面尚未提供 E610 镜像自动构建功能。适配包、构建记录和验收日志保存在服务器受限目录 `/home/ventoy-center/qa-centos7-e610-20261016/`，含凭据的 Kickstart 和任务 ISO 不提交仓库。

### 手动重装后的 E610 网络恢复（2026-10-16）

同日用户在 `.39` 手动重装后，系统仍为 `3.10.0-1160.el7.x86_64`，但安装环境的驱动没有自动部署到新系统。现场分别核对了驱动、连接配置和实际收发，不能仅凭 `modinfo` 输出判断网卡已经工作：

- 普通网卡 `ens4003f1` 的 DHCP 配置存在，但 `connection.autoconnect=no`；启用并保存后取得 `192.168.1.49/23`。
- 用户运行 r2 的 `install.sh` 后，磁盘上的模块及 initramfs 已是 ixgbe 6.4.5，但当前内核尚未加载 ixgbe。确认没有正在使用的 ixgbe 接口后执行 `modprobe ixgbe`，E610 的两个接口出现；接线的 `enp47s0f1`（MAC 尾号 `86:b2`）取得 `192.168.1.46/23`。连接配置按接口和 MAC 保存，启用开机自动连接。
- 两张网卡同时连接 `192.168.0.0/23`，装机服务器最初将 `.46` 解析成普通网卡的 MAC。仅获得 DHCP 地址仍不足以证明 E610 双向收发正常。对这两个接口分别持久设置 `arp_ignore=1`、`arp_announce=2`、`rp_filter=2`，再通过 E610 通告正确的地址归属，装机服务器解析恢复为 `b0:82:e2:00:86:b2`；指定 E610 接口访问服务器及网关均成功。

双网卡设置位于 `/etc/sysctl.d/90-ventoy-center-dual-nic.conf`，只针对本机两个接口；该调整用于本次同网段双网卡环境，不作为所有装机任务的默认配置。参数含义见 [Linux 内核网络参数说明](https://docs.kernel.org/networking/ip-sysctl.html)。用户此前要求的内核锁定和关闭自动更新策略也已在本次手动安装的系统中应用，实际 yum 排除项包含 `kernel*`。修复前配置与检查记录保存在目标机 `/root/ventoy-center-network-repair-20261016/`。

修复后已执行一次正常重启，boot ID 改变、`BootCurrent=0000` 仍为本地 CentOS，运行内核保持 1160、ixgbe 自动加载 6.4.5。两张网卡均自动取得上述地址，双网卡参数和更新策略检查通过；从 Windows 经 `.46` 的 SSH 密码登录成功，指定 E610 到装机服务器 3 次、到网关 2 次 ping 均无丢包，无失败 systemd 服务。E610 当前为 1 Gbps 全双工；本轮验证的是手动安装后的恢复流程，不新增其他系统版本的自动安装验收结果。
