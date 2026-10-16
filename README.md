# Ventoy Center

局域网批量装机管理端。客户端在 BIOS / UEFI 的 Boot 菜单选择网卡，由 **iVentoy / PXE** 加载镜像和自动安装配置；本项目负责目标机器、安装任务、授权、安装包和进度回传。

**当前代码与现场管理端：0.3.0-alpha.11，Linux 装机优先。** 本阶段只做直推装机，不修改 sckocstp / fyhocsys，也不创建新的自动接入任务。Windows 保留为次要支持，接入工作待装机流程完成后再考虑。

新生成的 Linux 自动安装任务默认锁定内核，并关闭软件源自动刷新和无人值守升级。普通 ISO 手动安装后，可在管理端“镜像管理”下载同一策略脚本执行。具体行为、检查与恢复见 [Linux 更新策略](docs/deployment/linux-update-policy.md)；新策略已通过隔离验证，尚不能代替此前各版本的完整实装回归。

服务端和安装包生成已实现；iVentoy 规则仍通过其原生界面配置。CentOS 7.9 已通过完整实机验收；Rocky 8.10 已完成实装与登录验证，启动网络等待修复也已通过回归。Ubuntu Server 22.04.3 已通过无人值守实装、自动硬盘启动、控制台 / SSH / sudo、完成回报和第二次重启验收。上述实装使用 iVentoy 1.0.44、Intel X710 网卡和 UEFI，其他镜像、硬件和启动模式仍需分别验收，见 [当前进度](docs/development/progress.md)。

alpha.8 为 Ubuntu 22.04 任务生成独立启动兼容包，并配置安装后的 EFI 启动顺序。2026-10-15 的 22.04.3 实机验收中，从选择专属任务镜像到安装完成、重启及回报，无需输入 `yes` 或手动调整启动顺序；保留盘分区表与签名保持一致。

现场 iVentoy 随后已升级并注册为 **1.0.45 专业版**，服务、授权、配置、镜像与任务绑定检查通过。上述完整实装仍是 1.0.44 的验收记录，1.0.45 尚未重新进行清盘安装，见[升级与回退记录](docs/deployment/iventoy-upgrade.md)。

2026-10-16，现场运行目录已统一为 `/home/ventoy-center/iventoy`，用户规划的 **18 个版本 ISO 已全部入库**。iVentoy 当前有 65 个入口，其中 59 个原始镜像、6 个已有任务镜像。此次补齐的 6 个 Debian / Ubuntu Server ISO 均核对官方 SHA-256 与内部版本；入库不代表完整实装验收，见[现场镜像清单](docs/deployment/image-catalog.md)。

## 已实现

- 中文管理页面、管理口令认证、机器登记、按 MAC 搜索和最近启动任务。
- 按目录浏览 / 搜索 ISO、文件名版本建议、系统模板目录；不把文件名识别视为镜像内容验证。
- 按 MAC、磁盘序列号、启动模式绑定安装任务；草稿需核对机器名和磁盘后授权。
- 24 小时任务凭据、单次安装领取、持久状态、取消与过期检查；服务重启不自动重发安装。
- 生成 CentOS 7.9、Rocky 8 / 9 / 10 Kickstart，Ubuntu Server 20.04 / 22.04 / 24.04 / 26.04 LTS autoinstall，Debian 11 / 12 / 13 preseed。
- Linux 安装前核对身份和磁盘、首次启动核对系统版本并回传安装状态。Debian 使用专门的安装环境保护脚本。
- 同机 iVentoy 文件准备：独立镜像硬链接、安装脚本、持久清单、失败重试和任务结束后的受限清理。
- Windows 安装前核对 MAC、磁盘序列号、磁盘编号和固件模式；需提前制作含所需可选组件的 WinPE。

## 当前边界

- **本阶段仅装机。** 新任务拒绝 `enroll: true`，界面不再要求节点包或连接文件。alpha.1 历史任务和状态仍保留，不自动迁移或执行。
- **安装包下载不是 PXE 下发。** 需要在 iVentoy 中关联镜像、脚本及按 MAC 的分机独启规则。尚无经过现场验证的 iVentoy 自动配置适配器。
- 每台机器生成独立任务。多台同时使用同一个源 ISO 时，配置不同 ISO 路径（可用硬链接），避免一个镜像的全局默认脚本串到其他机器。
- Windows ISO 的 WinPE 默认通常不含 PowerShell / StorageWMI，组件不足时安装保护会阻止继续。当前 Windows 模板语言支持 zh-CN / en-US，须与镜像一致。
- 当前 Linux 存储模板针对单块非 USB 磁盘。已有 LVM / RAID 成员需先人工处理；不会自动拆除可能跨盘的存储组。

## 服务端平台

| 平台 | 交付方式 | 当前验证范围 |
| --- | --- | --- |
| Rocky Linux 9（首要目标，包含用户的 9.8 环境） | 静态 Linux x86-64 二进制 + systemd | 9.8 同机服务、TLS 与管理接口已验证；已承载 CentOS 7.9、Rocky 8.10、Ubuntu 22.04.3 实装，具体边界见验收记录 |
| Rocky Linux 8 / 10 | 同一 Linux 二进制 | CI 启动检查，不等于 systemd / PXE 实机验收 |
| Debian 12 / 13 | 同一 Linux 二进制 + systemd | 同上 |
| Ubuntu 24.04 / 26.04 | 同一 Linux 二进制 + systemd | 同上；其他 24–26 间的短周期版本未单独验证 |
| Windows 10 / 11 x64 | Windows 二进制 + 可选计划任务 | Windows 单元测试；桌面系统与 iVentoy 共存需实机验收 |

这里列的是**装机服务器的运行系统**，与[客户端安装版本和镜像要求](docs/deployment/linux-targets.md)分开管理。当前按该页的 18 个具体版本逐项推进；CentOS Linux 8.5 和 Stream 9 / 10 已纳入后续计划。Ubuntu 仅支持 Server / live-server，不含 Desktop 或短周期版本。

## 本地启动

需要 Go 1.25 或更新版本；运行已编译程序不需要 Go、Node.js 或 Python。

```sh
go build -o ventoy-center ./cmd/ventoy-center
./ventoy-center init --dir ./runtime
./ventoy-center serve --config ./runtime/config.json
```

打开 `http://127.0.0.1:26080`，使用 `runtime/admin.token` 登录。将 ISO 按发行版放到 `runtime/iso/` 下，例如 `Rocky/Rocky-9.8-x86_64-dvd.iso`。

默认只监听回环地址。给局域网客户端使用之前，需要配置可达的 HTTPS 地址与证书；详见 [Rocky 9 部署](docs/deployment/rocky9.md)。Windows 命令将可执行文件改为 `ventoy-center.exe`。

## 文档

- [Rocky 9 与 fyhocsys 同机部署](docs/deployment/rocky9.md)
- [外部 DHCP、iVentoy 和安装包配置](docs/deployment/pxe.md)
- [同机准备 iVentoy 文件和权限配置](docs/deployment/iventoy-files.md)
- [iVentoy 1.0.45 现场升级与回退记录](docs/deployment/iventoy-upgrade.md)
- [Linux 安装版本、镜像和验收矩阵](docs/deployment/linux-targets.md)
- [现场镜像清单与新增 ISO 校验记录](docs/deployment/image-catalog.md)
- [CentOS 7.9 镜像、兼容处理与安装验证](docs/deployment/centos7.md)
- [Rocky 8.10 实机验收与启动回归](docs/deployment/rocky8.md)
- [Ubuntu 22.04.3 无人值守实机验收](docs/deployment/ubuntu22.md)
- [当前进度、客户端矩阵与后续工作](docs/development/progress.md)
- [Windows 与其他 Linux 服务端](docs/deployment/platforms.md)
- [架构、任务状态与尚未完成的能力](docs/development/architecture.md)
- [开发与验证](docs/development/testing.md)

本项目是独立编写的管理端，不包含 iVentoy、Windows ISO、fyhocsys 私有源码、节点包或任何授权文件。iVentoy 是单独安装和许可的软件；按 MAC 分机独启及商用需参考其[专业版说明](https://www.iventoy.com/cn/doc_edition.html)。
