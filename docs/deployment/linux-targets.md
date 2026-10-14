# Linux 客户端安装范围

当前优先完成系统安装。sckocstp / fyhocsys 代码、服务、数据库和授权不做修改，自动接入后置。服务器运行环境仍以现有 Rocky 9.8 为首要目标；本页列的是被安装客户端的系统。

## 模板目录

| 系统 | API profile | 安装器 | 镜像要求 |
| --- | --- | --- | --- |
| CentOS Linux 7.9 | `centos-7.9` | Kickstart | x86_64 DVD / Everything 2009 |
| Rocky Linux 8.x（至 8.10） | `rocky-8` | Kickstart | x86_64 完整 DVD |
| Rocky Linux 9.x（含 9.8，首要适配） | `rocky-9` | Kickstart | x86_64 完整 DVD |
| Rocky Linux 10.x（含 10.2） | `rocky-10` | Kickstart | x86_64 完整 DVD；CPU 满足 x86-64-v3 |
| Ubuntu Server 20.04 LTS | `ubuntu-20.04` | autoinstall | 20.04.6 amd64 live-server |
| Ubuntu Server 22.04 LTS | `ubuntu-22.04` | autoinstall | amd64 live-server |
| Ubuntu Server 24.04 LTS | `ubuntu-24.04` | autoinstall | amd64 live-server |
| Ubuntu Server 26.04 LTS | `ubuntu-26.04` | autoinstall | amd64 live-server |
| Debian 11 / Bullseye | `debian-11` | preseed | amd64 netinst / DVD-1 |
| Debian 12 / Bookworm | `debian-12` | preseed | amd64 netinst / DVD-1 |
| Debian 13 / Trixie | `debian-13` | preseed | amd64 netinst / DVD-1 |

这些 profile 均可创建任务和生成安装文件，覆盖 UEFI / Legacy BIOS 两种配置。**这是模板覆盖范围，不是各版本已经实机装通的声明**；具体 ISO、CPU、固件和网卡仍需分别验收。Rocky 8/9/10 的小版本共用同一大版本模板，不能据此认定每个历史 ISO 都已验证。Ubuntu Desktop、legacy-server、Debian Live、ARM 镜像不在此次范围。

Windows 11 / Server 2022 保留安装模板，开发优先级低于 Linux。Windows 11 限 UEFI，准备要求见 [PXE 文档](pxe.md)。Ubuntu 的短周期版本不列入当前范围。

## 当前优先清单（2026-10-13）

先完成 CentOS Linux 7.9 的完整安装验证，再推进以下其他版本。清单共 18 个具体版本；Ubuntu 仅使用 Server / live-server 镜像。现有其他模板继续保留，列入计划不代表已完成安装验收。

| 系统 | 计划准备和验收的版本 | 当前实现基础 |
| --- | --- | --- |
| CentOS Linux | **7.9（第一优先）**、8.5 | 7.9 官方 DVD 已通过 UEFI 虚拟机与真实硬件 PXE 完整安装、硬盘启动及登录验证；8.5 待单独适配 |
| CentOS Stream | 9、10 | 待单独适配版本校验与 Kickstart |
| Rocky Linux | 8.10、9.2、9.6、9.7、9.8 | 8 / 9 系列共用对应模板，逐 ISO 验收 |
| Debian | 12.8、12.15、13.0 | 12 / 13 系列共用对应模板，逐 ISO 验收 |
| Ubuntu Server | 22.04.3、22.04.4、22.04.5、24.04.3、24.04.4、24.04.5 | 22.04 / 24.04 系列共用对应模板，逐 ISO 验收 |

镜像从现有磁盘优先准备，先核对镜像内部版本、架构和校验和，再列入可引导目录。Desktop ISO 不能代替同版本 live-server ISO。

## 镜像选择和引导

将 ISO 放在服务器真实目录，例如 `iso/CentOS/`、`iso/Rocky/`、`iso/Ubuntu/`、`iso/Debian/`。目录和文件名不要含空格。页面展示目录树，并根据常见文件名建议版本；自定义镜像可以手动选 profile。文件名与已识别版本不符时，创建任务会被拒绝。

Kickstart 明确从 DVD 读取安装源；CentOS/Rocky 的仅网络引导 boot ISO 不适用于此模板。Ubuntu 需要可用的安装软件源。Debian 默认使用 `deb.debian.org/debian`，suite 固定为 bullseye / bookworm / trixie；如需内网镜像，绑定前修改包内 `mirror/http/hostname` 和 `mirror/http/directory`。Debian 安装器还需获取 `wget-udeb` 及依赖，不能仅准备最终系统的 DEB 包。

Debian 创建任务前必须设置 `bootstrap_ca_file`，即使控制器使用公共 CA，也应提供对应 PEM 信任链。精简安装环境通过该 CA 校验 HTTPS，不依赖 BusyBox wget，也不跳过证书验证。

客户端从 BIOS/UEFI 的 Boot 菜单选择网卡，进入 iVentoy，使用已关联的自动安装脚本。iVentoy 的镜像、脚本、分机独启和 MAC 过滤继续在其原生页面配置；本管理端的机器搜索不是网络层的 MAC 黑白名单。

## 写盘前和首次启动

所有 Linux 模板核对 MAC、唯一磁盘序列号、非 USB 磁盘和实际固件模式，再领取单次安装授权。当前存储方案只处理普通单盘；发现已有 LVM / RAID 成员时要求先人工准备，避免删除跨盘存储。

CentOS/Rocky/Ubuntu 还会核对安装环境的发行版。Debian 默认预置不存在的磁盘路径，只有所有检查和 HTTPS 领取成功后才把真实磁盘及分区确认写入 debconf。失败时钩子保持阻塞，可从其他终端查看 `/var/log/ventoy-center-guard.log`；已领取的任务不能重复使用。

安装完成后，首次启动脚本核对 hostname、发行版、版本及普通管理员账户，成功回传 `installed`。默认账户为 `deployadmin`，不要使用 Linux 内置的 `operator`。此状态不代表压测系统接入，也不代表全部驱动和业务软件健康。没有填写 SSH 公钥时，Ubuntu 允许普通用户用所设密码 SSH 登录；填写公钥时关闭 SSH 密码认证。

## 实机验收记录

每个 ISO 至少记录其 SHA-256、profile、CPU、网卡、磁盘、UEFI/BIOS、iVentoy 版本以及下列结果：

1. 从网卡进入正确镜像，自动安装配置被加载。
2. MAC 或磁盘不符、授权过期、控制器不可达时，未进行分区写入。
3. 正常任务只清空指定盘，安装后从磁盘启动，账号可登录，网络可用。
4. 控制器收到首次启动回报；旧任务再网启不能重复清盘。

2026-10-13，Rocky 9.7 经 LAN1 / UEFI PXE 进入安装欢迎页，因现有磁盘 UUID 冲突未继续安装。2026-10-14，CentOS 7.9 官方 DVD 已经同一网口、iVentoy 1.0.44 完成物理机自动安装、硬盘启动、控制台 / SSH / sudo 登录及完成回报，旧任务重放返回 HTTP 401。本轮先在授权下离线停止旧 RAID，仅准备目标盘，另一盘保留原有分区；模板仍拦截现有多盘存储。详见 [CentOS 7.9 虚拟机与实机验收记录](centos7.md)。其他具体 ISO 仍需逐项验收，CI 的操作系统容器只检查控制器启动。

## 官方依据

- [iVentoy 自动安装](https://www.iventoy.com/cn/doc_autoinstall.html)：Kickstart、Ubuntu cloud-init、Debian preseed 绑定方式。
- [Rocky 版本列表](https://docs.rockylinux.org/latest/releases/)及 [Rocky 10 CPU 要求](https://docs.rockylinux.org/latest/releases/release_notes/10_0/)。
- [CentOS 项目生命周期](https://www.centos.org/cl-vs-cs/)：CentOS 7 已于 2024-06-30 结束维护，保留模板用于用户现有兼容需求。
- [Ubuntu autoinstall 配置](https://canonical-subiquity.readthedocs-hosted.com/en/latest/reference/autoinstall-reference.html)。
- [Debian preseed 自定义命令](https://www.debian.org/releases/trixie/amd64/apbs05.en.html)与 [wget-udeb](https://packages.debian.org/trixie/wget-udeb)。
