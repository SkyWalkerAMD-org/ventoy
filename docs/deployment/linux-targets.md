# Linux 客户端安装范围

当前优先完成系统安装，默认使用 CLI，不安装桌面环境，并设置 `multi-user.target` 为启动目标。sckocstp / fyhocsys 代码、服务、数据库和授权不做修改，自动接入后置。服务器运行环境仍以现有 Rocky 9.8 为首要目标；本页列的是被安装客户端的系统。

## 模板目录

| 系统 | API profile | 安装器 | 镜像要求 |
| --- | --- | --- | --- |
| CentOS Linux 7.6 | `centos-7.6` | Kickstart | x86_64 DVD 1810 |
| CentOS Linux 7.9 | `centos-7.9` | Kickstart | x86_64 DVD / Everything 2009 |
| CentOS Linux 8.0 / 8.4 | `centos-8.0` / `centos-8.4` | Kickstart | x86_64 完整 DVD，分别核对小版本 |
| CentOS Linux 8.5.2111 | `centos-8.5` | Kickstart | x86_64 完整 DVD；与 Stream 区分 |
| CentOS Stream 8 | `centos-stream-8` | Kickstart | x86_64 完整 DVD，与 CentOS Linux 区分 |
| CentOS Stream 9 / 10 | `centos-stream-9` / `centos-stream-10` | Kickstart | x86_64 完整 DVD；10 需要 x86-64-v3 |
| Rocky Linux 8.x（至 8.10） | `rocky-8` | Kickstart | x86_64 完整 DVD |
| Rocky Linux 9.x（含 9.8，首要适配） | `rocky-9` | Kickstart | x86_64 完整 DVD |
| Rocky Linux 10.x（含 10.2） | `rocky-10` | Kickstart | x86_64 完整 DVD；CPU 满足 x86-64-v3 |
| AlmaLinux 8.x / 9.x | `almalinux-8` / `almalinux-9` | Kickstart | x86_64 完整 DVD |
| RHEL 7.x / 8.x / 9.x | `rhel-7` / `rhel-8` / `rhel-9` | Kickstart | x86_64 完整 Server DVD / DVD；不配置订阅 |
| Ubuntu Server 20.04 LTS | `ubuntu-20.04` | autoinstall | 20.04.3 及之后 amd64 live-server |
| Ubuntu Server 22.04 LTS | `ubuntu-22.04` | autoinstall | amd64 live-server |
| Ubuntu Server 24.04 LTS | `ubuntu-24.04` | autoinstall | amd64 live-server |
| Ubuntu Server 26.04 LTS | `ubuntu-26.04` | autoinstall | amd64 live-server |
| Ubuntu Server 24.10 / 25.04 | `ubuntu-24.10` / `ubuntu-25.04` | autoinstall | amd64 live-server；使用归档源 |
| Ubuntu Desktop 22.04 LTS | `ubuntu-22.04-desktop` | Ubiquity | 22.04.4 已通过隔离自动安装和 CLI 启动；镜像仍含桌面软件，常规无桌面装机优先用 Server |
| Ubuntu Desktop 26.04 LTS | `ubuntu-26.04-desktop` | Desktop Subiquity | amd64 Desktop；实验模板，不发布默认自动入口 |
| Debian 10 / Buster | `debian-10` | preseed | amd64 完整 DVD-1 |
| Debian 11 / Bullseye | `debian-11` | preseed | amd64 netinst / DVD-1 |
| Debian 12 / Bookworm | `debian-12` | preseed | amd64 netinst / DVD-1 |
| Debian 13 / Trixie | `debian-13` | preseed | amd64 netinst / DVD-1 |

这些 profile 均可创建任务和生成安装文件，覆盖 UEFI / Legacy BIOS 两种配置。**这是模板覆盖范围，不是各版本已经实机装通的声明**；具体 ISO、CPU、固件和网卡仍需分别验收。Rocky 8/9/10 的小版本共用同一大版本模板，不能据此认定每个历史 ISO 都已验证。默认使用无桌面的 Server / 标准 / 最小化安装。alpha.25 另验证 Ubuntu Desktop 22.04.4 的原生安装及自动安装，设置为 CLI 启动；这不等同于删除镜像中全部桌面软件。Ubuntu Desktop 26.04 仍保留实验模板，不计入本轮自动安装验收。legacy-server、Debian Live、ARM 镜像不在此次范围。

Windows 11 / Server 2022 保留安装模板，开发优先级低于 Linux。Windows 11 限 UEFI，准备要求见 [PXE 文档](pxe.md)。现场已有的 Ubuntu 24.10、25.04 已加入 alpha.21 回归范围。

## 当前优先清单（2026-10-24）

alpha.25 已为库存中 43 个需要 E610 适配的原始 ISO 完成内置驱动，手动/PXE 自动安装及再次硬盘启动共 86/86 项隔离验收通过。现场有 52 个自动入口，普通入口与自动入口复用相应成品。逐镜像版本、安装内核与哈希见 [E610 成品清单](https://github.com/SkyWalkerAMD-org/E610/blob/codex/e610-pxe-media/pxe-media/baked-catalog.json)；本轮无 E610 实卡，物理网络仍待验收。以下保留规划清单及此前结果。

清单共 18 个规划版本；Ubuntu 仅使用 Server / live-server 镜像。CentOS 7.9、Rocky 8.10、Rocky 9.2、Ubuntu 22.04.3 已有完整物理无人值守实装证据。alpha.20 / alpha.21 已完成规划版本及 9 个库存 CLI 版本的隔离完整安装与两次启动，现场 12 个 Ubuntu Server（含 E610）和 7 个 Debian DVD 自动入口均已启用。默认安装无 GUI，内核锁定与关闭自动更新策略保持启用。物理验收与隔离验收分别记录在[当前进度](../development/progress.md)。模板覆盖不代表每个 ISO、硬件和固件组合均已验收。

这 18 个版本的 ISO 已全部入库，并已准备自动安装入口，见[现场镜像路径与校验记录](image-catalog.md)。当前流程为“选择系统 → 选择自动脚本 → 自动安装”：仅一块可用内置盘时直接安装；多盘在 PXE 本机选盘、输入 `yes`，或选择双盘 RAID1 + LVM，无需先到后台指定。alpha.24 加入 AlmaLinux 8.10 / 9.5，RHEL 7.6 / 7.9、8.0 / 8.1 / 8.6 / 8.10、9.2 / 9.4，当时共 39 个 CLI 自动入口。RHEL 7.9、8.10、9.4 与 AlmaLinux 8.10、9.5 另有隔离双盘实装；alpha.25 为上述各 ISO 补全 E610 成品的单盘手动/自动安装验收，两类结果分别记录。

| 系统 | 计划准备和验收的版本 | 当前实现基础 |
| --- | --- | --- |
| CentOS Linux | 7.9、8.5 | 均有隔离 UEFI 自动安装与两次启动验证；7.9 另有物理 PXE 验收 |
| CentOS Stream | 9、10 | 独立版本检查、Kickstart 和隔离 UEFI 自动安装与两次启动验证通过 |
| Rocky Linux | 8.10、9.2、9.6、9.7、9.8 | 五个版本均通过隔离 UEFI 自动安装与两次启动验证；8.10、9.2 另有物理 PXE 和网络等待修复验收 |
| Debian | 12.8、12.15、13.0 | 三个 DVD-1 均通过隔离完整自动安装、两次启动、内核锁定和 APT / 无人值守升级禁用检查 |
| Ubuntu Server | 22.04.3、22.04.4、22.04.5、24.04.3、24.04.4、24.04.5 | 六个版本均通过隔离完整自动安装、两次启动、镜像内核锁定和更新禁用检查；22.04.3 另有物理 PXE 验收 |

镜像从现有磁盘优先准备，先核对镜像内部版本、架构和校验和，再列入可引导目录。Desktop ISO 不能代替同版本 live-server ISO。

## 镜像选择和引导

将 ISO 放在服务器真实目录，例如 `iso/CentOS/`、`iso/Rocky/`、`iso/Ubuntu/`、`iso/Debian/`。目录和文件名不要含空格。页面展示目录树，并根据常见文件名建议版本；自定义镜像可以手动选 profile。文件名与已识别版本不符时，创建任务会被拒绝。

Kickstart 明确从 DVD 读取安装源；CentOS/Rocky 的仅网络引导 boot ISO 不适用于此模板。Ubuntu 需要可用的安装软件源。alpha.21 中 Debian DVD-1 从完整介质安装，不在安装阶段刷新外部软件源；netinst 仍使用 `deb.debian.org/debian`，suite 固定为 bullseye / bookworm / trixie；如需内网镜像，绑定前修改包内 `mirror/http/hostname` 和 `mirror/http/directory`。Debian 安装器还需获取 `wget-udeb` 及依赖，不能仅准备最终系统的 DEB 包。

Debian 创建任务前必须设置 `bootstrap_ca_file`，即使控制器使用公共 CA，也应提供对应 PEM 信任链。精简安装环境通过该 CA 校验 HTTPS，不依赖 BusyBox wget，也不跳过证书验证。

客户端从 BIOS/UEFI 的 Boot 菜单选择网卡，进入 iVentoy，使用已关联的自动安装脚本。iVentoy 的镜像、脚本、分机独启和 MAC 过滤继续在其原生页面配置；本管理端的机器搜索不是网络层的 MAC 黑白名单。

## 写盘前和首次启动

所有 Linux 模板核对 MAC、唯一磁盘序列号、非 USB 磁盘和实际固件模式，再领取单次安装授权。单盘方案保护已有 LVM / RAID；alpha.24 的双盘 RAID1 + LVM 方案允许在确认后清理完全包含于所选两盘的旧布局，重新建立镜像和 LVM，不保留旧数据或原分区大小。缺失成员、第三块未选成员、挂载或交换空间仍会中止安装。详见[PXE 双盘流程](pxe-menu.md)。

Linux 模板还会核对安装环境的发行版，Debian 使用安装器的 `/etc/lsb-release` 标识。Debian 默认预置不存在的磁盘路径，只有所有检查和 HTTPS 领取成功后才把真实磁盘及分区确认写入 debconf。失败时钩子保持阻塞，可从其他终端查看 `/var/log/ventoy-center-guard.log`；已领取的任务不能重复使用。

安装完成后，首次启动脚本核对 hostname、发行版、版本、普通管理员账户和更新策略，成功回传 `installed`。当前新建表单默认普通管理员为 `user`，现场另配置 `linux_admin_username` 强制后续 Linux 任务使用该名称；管理员及 root 初始密码由服务端统一策略提供。此状态不代表压测系统接入，也不代表全部驱动和业务软件健康。alpha.22 中配置了 root 密码的 Ubuntu / Debian 同时允许 SSH 密码登录，并在首次启动检查其生效；管理员公钥仍可使用。未配置 root 密码时，Ubuntu 的 SSH 密码认证继续由是否提供公钥决定。

## 实机验收记录

每个 ISO 至少记录其 SHA-256、profile、CPU、网卡、磁盘、UEFI/BIOS、iVentoy 版本以及下列结果：

1. 从网卡进入正确镜像，自动安装配置被加载。
2. MAC 或磁盘不符、授权过期、控制器不可达时，未进行分区写入。
3. 正常任务只清空指定盘，安装后从磁盘启动，账号可登录，网络可用。
4. 控制器收到首次启动回报；旧任务再网启不能重复清盘。

2026-10-13，Rocky 9.7 经 LAN1 / UEFI PXE 进入安装欢迎页，因现有磁盘 UUID 冲突未继续安装。2026-10-14，CentOS 7.9 官方 DVD 已经同一网口、iVentoy 1.0.44 完成物理机自动安装、硬盘启动、控制台 / SSH / sudo 登录及完成回报，旧任务重放返回 HTTP 401。本轮先在授权下离线停止旧 RAID，仅准备目标盘，另一盘保留原有分区；模板仍拦截现有多盘存储。详见 [CentOS 7.9 虚拟机与实机验收记录](centos7.md)。

同日，Rocky 8.10 DVD 完成同一硬件上的自动安装、硬盘启动、登录及完成回报，保留盘分区信息一致。initramfs 曾等待已改名的网卡约 10 分钟；2026-10-15 的完整实机回归确认修复有效，initrd 阶段为 12.733 秒，登录、完成回报与保留盘检查再次通过。详见 [Rocky 8.10 验收记录](rocky8.md)。其他具体 ISO 仍需逐项验收，CI 的操作系统容器只检查控制器启动。

2026-10-15，Ubuntu Server 22.04.3 已在同一硬件通过 alpha.8 无人值守实装、自动 UEFI 硬盘启动、控制台 / SSH / sudo 登录、完成回报与第二次重启；没有输入 `yes` 或在安装后手动调整启动顺序。旧授权重放被拒绝，保留盘分区表和签名一致。详见 [Ubuntu 22.04.3 记录](ubuntu22.md)。

2026-10-16，Rocky 9.2 在 iVentoy 1.0.45 Pro / alpha.10 下通过全新无人值守实装、自动硬盘启动、控制台 / SSH / sudo、完成回报、旧授权拒绝及第二次正常重启。网络等待修复在安装时自动应用，两次硬盘启动均无失败服务，保留盘分区表及签名与基线一致。详见 [Rocky 9 客户端记录](rocky9-client.md)。

## 官方依据

- [iVentoy 自动安装](https://www.iventoy.com/cn/doc_autoinstall.html)：Kickstart、Ubuntu cloud-init、Debian preseed 绑定方式。
- [Rocky 版本列表](https://docs.rockylinux.org/latest/releases/)及 [Rocky 10 CPU 要求](https://docs.rockylinux.org/latest/releases/release_notes/10_0/)。
- [CentOS 项目生命周期](https://www.centos.org/cl-vs-cs/)：CentOS 7 已于 2024-06-30 结束维护，保留模板用于用户现有兼容需求。
- [Ubuntu autoinstall 配置](https://canonical-subiquity.readthedocs-hosted.com/en/latest/reference/autoinstall-reference.html)。
- [Debian preseed 自定义命令](https://www.debian.org/releases/trixie/amd64/apbs05.en.html)与 [wget-udeb](https://packages.debian.org/trixie/wget-udeb)。
