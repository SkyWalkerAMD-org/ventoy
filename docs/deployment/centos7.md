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

兼容包仅为 CentOS 7.9 任务生成，仍需通过 MAC、磁盘序列号、现有 RAID/LVM 和一次性授权检查。旧任务的文件清单不会被自动覆盖；缺少兼容包时取消并清理旧任务，重新生成。兼容入口的真实硬件验证正在进行，尚不能视作完成安装验收。

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

同日，真实硬件已通过 Intel X710 的 LAN1 / UEFI PXE，经 iVentoy 1.0.44 加载上述 DVD 并进入 CentOS 7 安装欢迎 / 语言页。服务器日志确认客户端正在读取该 CentOS ISO。测试机使用 Xeon w7-2495X、64 GiB 内存和两块 HFS480G3H2X069N SATA 磁盘；安装环境启动时出现既有 LVM 的扫描错误，正式安装前需单独核对旧存储。

2026-10-14，已在用户授权后按序列号准备一块测试盘，另一块盘保留。专属任务镜像与 Kickstart 已绑定，客户端也显示并选择了对应脚本；但本轮 iVentoy 1.0.44 的 `Standard` 模式启动仍进入交互式欢迎页，安装器命令行没有 `inst.ks`，管理端未收到写盘前的授权领取。脚本文件经 iVentoy HTTP 服务读取后与原文件一致，不能把“绑定成功”当作“安装器已加载脚本”。此为现场观察，尚未证实由安全启动支持模式导致。

后续重新核对时，两盘已被另一轮 Debian 安装配置为软件 RAID，原先“目标盘已准备”的状态不再成立。本轮重新进入 CentOS 安装器后确认，两盘均未挂载，阵列未组装，但 RAID 成员标记仍在；本轮尚未清除标记。临时切换 `Not Supported` 后问题仍复现，已恢复 `Standard`。客户端日志中的 `sed: /etc/vtoycmdline: No such file or directory` 和已上传的完整 Kickstart 指向上述启动参数兼容问题，专属镜像已绑定兼容包等待复验。

兼容包首次实机复验在 `Standard` 模式下已读取 Kickstart，`/tmp/ks.cfg` 和 `/run/install/ks.cfg` 均为 10310 字节；随后约 247 秒报告 initqueue 超时。客户端回传的报告显示 `80-kickstart.conf` 含 `ip=eth1:dhcp: bootdev=eth1`，实际联网设备已是 `eno2` 且已取得局域网地址，ISO 也已只读挂载。任务仍为待启动，未领取写盘授权。已据此修正网络配置并通过原始解析器对照实验；修正后的物理网启、完整安装仍需验收，不能标记为实装通过。

真实硬件的网启到安装器已验证；原生脚本注入、完整安装、硬盘启动和登录仍需完成验收。UEFI 下的结果也不代表 Legacy BIOS 或 Secure Boot 已通过。
