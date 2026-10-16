# 现场镜像清单

核对日期：2026-10-16。现场为 iVentoy **1.0.45 专业版**，实际运行目录为 `/home/ventoy-center/iventoy`，镜像根目录为其下的 `iso/`。

用户规划的 **18 个版本已全部入库**，同时出现在 iVentoy 和 Ventoy Center 的镜像列表中。iVentoy 共 65 个入口：59 个原始镜像、6 个已有任务的专属镜像硬链接。管理端镜像列表排除任务目录，因此显示 59 个原始镜像。

本次补齐 6 个 ISO，没有创建安装任务。入库仅证明镜像存在、可被两端识别；CentOS 7.9、Rocky 8.10、Ubuntu Server 22.04.3 的完整实装记录仍基于 iVentoy 1.0.44。其他 15 个版本及 1.0.45 的完整安装需另行验收，详见[当前进度](../development/progress.md)。

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
