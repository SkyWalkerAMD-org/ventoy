# 现场镜像清单

核对日期：2026-10-25。现场为 iVentoy **1.0.47 专业版**、Ventoy Center **0.3.0-alpha.33**，实际运行目录为 `/home/ventoy-center/iventoy`，镜像根目录为其下的 `iso/`。

当前为 **109 个路径：58 个基础系统路径、1 个 MemTest86 工具镜像、50 个有效 PXE 自动安装入口**。基础系统包括 52 个不重复的 Linux 镜像和 6 个 Windows 镜像。Linux 中 43 个已内置 E610 驱动、原生安装收尾钩子与手动配置包，9 个原生支持 E610，保留原驱动和当前预置绑定。所有现有自动入口均与对应基础镜像使用同一份文件内容。

2026-10-25 核对 CentOS 7.9 和 Ubuntu 22.04.3 各自的普通名称与 E610 名称：两者是同一 inode 的硬链接，内容完全相同。按用户要求移除下列两个基础别名及其两个 PXE-Auto 入口，菜单由 113 减为 109；普通名称保留的仍是 E610 适配成品。

| 保留的基础入口 | 移除的重复基础入口 | 移除的自动入口编号 |
| --- | --- | --- |
| `CentOS/CentOS-7.9-x86_64-DVD-2009.iso` | `CentOS/CentOS-7.9-x86_64-E610-r2-DVD.iso` | `33523ae47b77e36178ca2ca2` |
| `Ubuntu/ubuntu-22.04.3-live-server-amd64.iso` | `Ubuntu/ubuntu-22.04.3-live-server-amd64-E610.iso` | `750c70aa968061f129fd658b` |

清理等待活动安装结束，并通过 SSH 核实 `.235` 已从硬盘启动、未挂载安装镜像后处理其旧连接。移除文件和脚本在 ISO 目录外保留恢复副本；50 个保留自动入口的脚本摘要、原生绑定、镜像身份以及全局配置核对一致，历史机器和任务记录保留。备份与验收位于 `/home/ventoy-center/e610-pxe-media-20261023/dedup-20261025/`（`plan.json`、`result.json`、`backup-20261025-135641Z/`）。这次清理减少菜单重复，不代表释放两份完整 ISO 的磁盘空间。

9 个原生支持镜像为 Rocky 9.6 / 9.7 / 9.8 / 10.2，以及 Ubuntu Server 24.04.4 / 24.04.5 / 25.04 / 26.04 和 Ubuntu Desktop 26.04。43 个适配成品已通过 86 项隔离安装与重启检查；当前没有 E610 实卡，不把它计为所有版本的物理网口验收，具体见[当前进度](../development/progress.md)。Windows 和 MemTest86 不属于 Linux 驱动注入范围。

alpha.25 曾增加 13 个自动入口：CentOS 7.6 / 8.0 / 8.4 / Stream 8，Rocky 10.0 / 8.6 / 8.7 / 8.9 / 9.1 / 9.3 / 9.4 / 9.5，以及 Ubuntu Desktop 22.04.4 CLI 启动入口。Desktop 仍含桌面软件，常规无桌面装机优先选择 Server。以下保留各次发布的历史计数与验证记录。

原版镜像及其原权限、旧管理端、配置和绑定备份位于 `/home/ventoy-center/e610-pxe-media-20261023/production-backup-20261024-195208/`。菜单中的适配成品统一允许管理端账户读取，避免部分旧文件仅 root 可读导致无法创建自动入口。移除适配路径重复的外部手动注入绑定；驱动和配置包均已在 ISO 内。验收记录为同级任务目录的 `production-acceptance.json`、`production-postcheck.json`；逐 ISO 哈希与安装内核见 [E610 成品清单](https://github.com/SkyWalkerAMD-org/E610/blob/codex/e610-pxe-media/pxe-media/baked-catalog.json)。

MemTest86 位于 `memtest86/MemTest86-11.7-Free-UEFI.iso`，SHA-256 为 `c204d55cc5b46fb164aa1a7f7c46101a988a40691e5dcf61a7003ed5d4504fb0`，大小 274696192 字节。它由用户提供的官方 USB 包制作成 UEFI ISO，保留原 EFI 启动内容，已验证启动，不等同于完成整机内存测试。

此前同日按“保留全部原版 ISO，只清理重复和历史测试残留”清理了 6 个已取消任务的入口、脚本和空目录。iVentoy 从 **86 个入口减为 80 个**，随后发布 9 个完成验收的库存 CLI 自动安装入口，达到 **89 个路径：60 个基础系统镜像、29 个有效 PXE 自动安装入口**。`ventoy-center/` 下仅保留 `PXE-Auto/`；CentOS 和 Ubuntu 的 E610 成功基础镜像及有效自动入口均保留。默认自动入口均不安装 GUI；原版 Desktop ISO 保留手动选择。

本次通过受管清理接口移除 `82080068857489a6c9dcd895`、`92ff3ac36ea2f3d27a3f93c9`、`d2046d624dc2c35e773aafe5`、`d2101c3c22bab47bab39230c`、`d80f480bb5664c2e61436b32` 和 `df64478d3ba75899463302dc`。核对了 60 个基础 ISO 的路径、设备号、inode、大小和修改时间，以及 80 个保留入口的脚本/注入绑定和全局设置。清理前确认唯一物理客户端 `.235` 已关机，完成后刷新 iVentoy 并恢复网启。任务记录和安装结果仍保留，备份位于服务器 `/home/ventoy-center/image-cleanup-1792725034796916770/`。

alpha.21 发布的 29 个入口包含 CentOS 5 个、Rocky 5 个、Debian 7 个、Ubuntu Server 12 个（包含两个 E610 入口）。9 个新增入口使用现有 ISO 的硬链接，不复制整份镜像。29 项脚本与注入绑定、所有文件摘要、全局配置和其他入口设置均核对通过；发布备份与验收记录位于 `/home/ventoy-center/qa-alpha21/`。

以下保留此前入库和清理记录；其中的计数描述当时状态。

用户规划的 **18 个版本已全部入库**，同时出现在 iVentoy 和 Ventoy Center 的镜像列表中。2026-10-16 按用户要求清理历史测试残留后，iVentoy 共 **61 个入口：59 个原版镜像、1 个 E610 基础适配镜像和 1 个 Rocky 9.6 待测任务入口**。全部原版 ISO 保留；管理端镜像列表排除任务目录，共 60 个基础镜像。

补齐 6 个 ISO 的入库操作没有创建安装任务。入库仅证明镜像存在、可被两端识别；之后 Rocky 9.2 和 CentOS 7.9 E610 适配镜像已在 iVentoy 1.0.45 Pro 完成实装。18 个具体版本中仍有 14 个待完整实装，详见[当前进度](../development/progress.md)。

## 18 个计划版本

下表路径相对于 `/home/ventoy-center/iventoy/iso/`。Ubuntu 全部为 Server / live-server；Debian 新增镜像为 amd64 DVD-1，未使用 Desktop / Live 替代。

| 版本 | 镜像路径 | 本次处理 |
| --- | --- | --- |
| CentOS 7.9 | `CentOS/CentOS-7.9-x86_64-DVD-2009.iso` | 原有 |
| CentOS Linux 8.5 | `CentOS/CentOS-8.5.2111-x86_64-dvd1.iso` | 原有 |
| CentOS Stream 9 | `CentOS/CentOS-Stream-9-latest-x86_64-dvd1.iso` | 原有 |
| CentOS Stream 10 | `CentOS/CentOS-Stream-10-latest-x86_64-dvd1.iso` | 原有 |
| Rocky 8.10 | `Rocky/Rocky-8.10-x86_64-dvd.iso` | 原有 |
| Rocky 9.2 | `Rocky/Rocky-9.2-x86_64-dvd.iso` | 原有 |
| Rocky 9.6 | `Rocky/Rocky-9.6-x86_64-dvd.iso` | 原有 |
| Rocky 9.7 | `Rocky/Rocky-9.7-x86_64-dvd.iso` | 原有 |
| Rocky 9.8 | `Rocky/Rocky-9.8-x86_64-dvd.iso` | 原有 |
| Debian 12.8 | `Debian/debian-12.8.0-amd64-DVD-1.iso` | 新增并校验 |
| Debian 12.15 | `Debian/debian-12.15.0-amd64-DVD-1.iso` | 新增并校验 |
| Debian 13.0 | `Debian/debian-13.0.0-amd64-DVD-1.iso` | 新增并校验 |
| Ubuntu Server 22.04.3 | `Ubuntu/ubuntu-22.04.3-live-server-amd64.iso` | 原有 |
| Ubuntu Server 22.04.4 | `Ubuntu/ubuntu-22.04.4-live-server-amd64.iso` | 原有 |
| Ubuntu Server 22.04.5 | `Ubuntu/ubuntu-22.04.5-live-server-amd64.iso` | 新增并校验 |
| Ubuntu Server 24.04.3 | `Ubuntu/ubuntu-24.04.3-live-server-amd64.iso` | 原有 |
| Ubuntu Server 24.04.4 | `Ubuntu/ubuntu-24.04.4-live-server-amd64.iso` | 新增并校验 |
| Ubuntu Server 24.04.5 | `Ubuntu/ubuntu-24.04.5-live-server-amd64.iso` | 新增并校验 |

CentOS Linux 8.5、Stream 9 / 10 的独立无人值守 profile、版本校验与 Kickstart 已在 alpha.9 实现，尚未完成实装。现场已核对这三个 ISO 内的发行版 RPM 和 `minimal-environment` 软件组。其他版本的模板基础见 [Linux 客户端范围](linux-targets.md)。

另有 `CentOS/CentOS-7.9-x86_64-E610-r2-DVD.iso`，用于 E610 网卡的安装环境适配，不替换上表中的原版 CentOS DVD。其基础入口不会自动清盘，也不会替手动安装执行目标系统驱动持久化；无人值守需要新机器的独立任务镜像。`.39` 已用完的专属 `CentOS-7.9-E610-AUTO-BMC39.iso` 已从菜单移除并保留私有验收副本。详见 [E610 实装记录](centos7.md#intel-e610-实机适配2026-10-16)。

## 历史测试残留清理（2026-10-16）

按用户确认的“保留全部原版 ISO，只清理重复和历史测试残留”执行。`ventoy-center` 是每台机器无人值守任务的专用目录，其中的 `image.iso` 为原版镜像的硬链接；它与任务脚本配合使用，不能把已完成任务的入口当成其他机器的通用安装镜像。

本次解除 9 个已结束任务的脚本及注入包绑定，再通过管理端受管文件清理接口删除其 ISO 硬链接、配套脚本与空目录。涉及 CentOS 7.9 的 1 个、Rocky 8.10 的 2 个、Ubuntu 22.04.3 的 3 个及 Rocky 9.2 的 3 个历史任务；安装结果、事件、机器信息和其他任务状态全部保留。

保留 Rocky 9.6 待测任务 `d2101c3c22bab47bab39230c` 的镜像入口和脚本。该任务尚未完成原生绑定与实装验收。E610 成功基础镜像仍在 `CentOS` 目录，`.39` 成功任务 ISO 仍存放于服务器受限验收目录；已删除不再使用的 16,893,952 字节 `E610-CentOS7.9-Driver-Test.iso` 及 Windows 工作区中的同内容临时副本。

清理后核对：iVentoy 入口从 70 减为 61，60 个基础镜像的路径、设备号、inode、字节数及修改时间均未变化，待测任务脚本摘要未变化。基础镜像之间没有相同字节数的文件，因此未发现内容完全相同的重复基础 ISO。删除任务硬链接主要减少菜单残留，不会释放对应原版 ISO 的数据空间。

网启短暂停止以解除旧绑定，随后恢复并刷新镜像列表。管理端与 iVentoy 服务均正常；全局参数与清理前一致。清理清单、绑定与脚本备份、前后文件元数据和执行结果保存在服务器受限目录 `/home/ventoy-center/iso-cleanup-20261016/`。

## 本次新增镜像的校验

通过发行版官方 HTTPS 目录获取镜像和 `SHA256SUMS`，下载在镜像目录外完成。逐个核对字节数、SHA-256 与 ISO 内的 `.disk/info`，确认版本、amd64 架构及 Server / DVD-1 类型后才入库。本次未做清单的 GPG 签名验证。

| 文件 | 字节数 | 官方校验清单 |
| --- | ---: | --- |
| `debian-12.8.0-amd64-DVD-1.iso` | 3998007296 | [Debian 12.8.0 SHA256SUMS](https://cdimage.debian.org/cdimage/archive/12.8.0/amd64/iso-dvd/SHA256SUMS) |
| `debian-12.15.0-amd64-DVD-1.iso` | 3989078016 | [Debian 12.15.0 SHA256SUMS](https://cdimage.debian.org/cdimage/archive/12.15.0/amd64/iso-dvd/SHA256SUMS) |
| `debian-13.0.0-amd64-DVD-1.iso` | 3994091520 | [Debian 13.0.0 SHA256SUMS](https://cdimage.debian.org/cdimage/archive/13.0.0/amd64/iso-dvd/SHA256SUMS) |
| `ubuntu-22.04.5-live-server-amd64.iso` | 2136926208 | [Ubuntu 22.04.5 SHA256SUMS](https://releases.ubuntu.com/22.04.5/SHA256SUMS) |
| `ubuntu-24.04.4-live-server-amd64.iso` | 3405469696 | [Ubuntu 24.04.4 SHA256SUMS](https://releases.ubuntu.com/24.04.4/SHA256SUMS) |
| `ubuntu-24.04.5-live-server-amd64.iso` | 4080486400 | [Ubuntu 24.04.5 SHA256SUMS](https://releases.ubuntu.com/24.04.5/SHA256SUMS) |

已核对的 SHA-256：

```text
eadbecad1801066111cf2c654a76cdc49fb6ee8d7683dd4492eeadb8cb5348ba  debian-12.8.0-amd64-DVD-1.iso
484e1250d3dd29c8d7f83f30a08efb22f922830b8fe9f1e299e89a21c895955f  debian-12.15.0-amd64-DVD-1.iso
c998fe73ca8dbce235f189a2a92873bf0a8f70b0982f361629a18a0f38b6fe92  debian-13.0.0-amd64-DVD-1.iso
9bc6028870aef3f74f4e16b900008179e78b130e6b0b9a140635434a46aa98b0  ubuntu-22.04.5-live-server-amd64.iso
e907d92eeec9df64163a7e454cbc8d7755e8ddc7ed42f99dbc80c40f1a138433  ubuntu-24.04.4-live-server-amd64.iso
97f3d7ffb032c3eb3b23d2c8be9cc76e60c2c1f2c0146ba5ba9fe01cafae0fd8  ubuntu-24.04.5-live-server-amd64.iso
```

ISO 内部信息分别确认 Debian 12.8.0 / 12.15.0 Bookworm、13.0.0 Trixie，以及 Ubuntu-Server 22.04.5 Jammy / 24.04.4 与 24.04.5 Noble。管理端版本建议分别为 `debian-12`、`debian-13`、`ubuntu-22.04`、`ubuntu-24.04`。

## 入库与现有配置检查

验证后在同一文件系统内建立 ISO 硬链接，文件属于 `ventoy-center`、权限 `0440`。没有安装中的任务或正在使用网络镜像的客户端时，重启 iVentoy 刷新目录，随后检查两端清单。

- 原有 59 个 iVentoy 入口及脚本、注入包、默认脚本、超时等绑定保留。
- 新增 6 个 ISO 未关联无人值守脚本，未设置为全局默认启动镜像。
- ProxyNet、Standard Secure Boot、全局默认镜像为空等原有配置保留，专业版授权有效。
- 管理端任务状态文件与入库前一致，两个服务均正常运行且保持开机启动。

下载记录、官方校验清单、ISO 内部信息及导入结果保存在服务器受限目录 `/home/ventoy-center/catalog-rename-20261016/`。`image-import-result.json` 记录六个文件的来源、摘要及 18 版覆盖检查。该目录中的原始下载与正式目录为硬链接，不算独立数据备份。目录改名的备份和回退步骤见 [iVentoy 升级与目录迁移记录](iventoy-upgrade.md)。

## alpha.20 通用自动安装入口（2026-10-23）

本轮为 18 个规划标准版本和两个保留的 E610 适配版本准备并启用共 20 个自动安装入口。`ventoy-center/PXE-Auto/` 下的目录名称包含完整系统版本，使用原版 ISO 的硬链接；原版内容、所有者和权限保持不变。新增 17 个入口后，原有 69 个镜像路径增加至 86 个。

覆盖 CentOS 7.9 / 8.5 / Stream 9 / Stream 10，Rocky 8.10 / 9.2 / 9.6 / 9.7 / 9.8，Debian DVD 12.8.0 / 12.15.0 / 13.0.0，以及 Ubuntu Server 22.04.3 / 22.04.4 / 22.04.5 / 24.04.3 / 24.04.4 / 24.04.5；另外保留 CentOS 7.9 E610 与 Ubuntu Server 22.04.3 E610。

20 项脚本与所需注入包均通过文件摘要及原生 iVentoy 绑定检查。默认脚本编号为 1、脚本选择超时为 0；全局默认镜像为空、全局倒计时为 0。各入口采用 `single-internal` 规则，选择自动脚本后按规则安装；原版 ISO 没有新增自动安装绑定，其他镜像参数核对保留。

此目录更新没有重装任何物理客户端。各版本的隔离完整安装和物理验收分别见[当前进度](../development/progress.md)。备份、配置核对及验收证据保存在服务器私有目录 `/home/ventoy-center/qa-alpha20/`。
