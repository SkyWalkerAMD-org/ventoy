# Ventoy Center 当前进度

更新日期：2026-10-15。现场管理端为 `0.3.0-alpha.8`，部署在现有 Rocky Linux 9.8 服务器上。Rocky 8.10 启动等待修复回归与 Ubuntu Server 22.04.3 无人值守实机验收均已完成。本阶段只做直推装机，未修改 sckocstp / fyhocsys，也未开启装机后接入。

## 已交付的流程

管理页面可登记机器，按 MAC、目标磁盘序列号和固件模式创建安装任务，授权后生成每台机器独立的安装文件，并在同机 iVentoy 目录准备镜像硬链接与脚本。客户端从网卡进入 iVentoy 后加载安装脚本，安装前领取单次授权，首次从硬盘启动后回报结果。

管理端已有 HTTPS、管理口令、持久任务状态、授权有效期、禁止重复领取、人工失败结单及受管文件清理。iVentoy 的镜像与脚本关联仍通过其原生页面配置；现场辅助操作脚本不等于产品已实现网页一键下发。

## 客户端验收矩阵

以下为用户当前优先清单，共 18 个具体版本。模板可生成、ISO 已准备、进入安装器和完整实装分别记录，不互相替代。

| 版本 | 当前证据 | 尚需完成 |
| --- | --- | --- |
| CentOS 7.9 | 官方 DVD、UEFI 实机自动安装、硬盘启动、控制台 / SSH / sudo、完成回报及旧授权重放拒绝均通过 | 其他硬件、Legacy BIOS、批量并发 |
| CentOS Linux 8.5 | 已纳入计划 | 独立 profile、版本保护、安装模板与完整实装 |
| CentOS Stream 9、10 | 已纳入计划 | 独立 profile、版本保护、安装模板与完整实装 |
| Rocky 8.10 | UEFI 实机自动安装、硬盘启动、登录及完成回报通过；网络等待修复回归通过，initrd 阶段降到 12.733 秒 | 其他硬件、Legacy BIOS、批量并发 |
| Rocky 9.2、9.6、9.8 | 共用 Rocky 9 模板 | 每个 ISO 完整实装 |
| Rocky 9.7 | 已通过 PXE 到达安装器；当时因旧磁盘 UUID 重复停止 | 完整实装和首次启动验收 |
| Debian 12.8、12.15、13.0 | 对应大版本的 preseed 模板已实现 | 每个 ISO 完整实装 |
| Ubuntu Server 22.04.3 | alpha.8 无人值守实装、自动 UEFI 硬盘启动、控制台 / SSH / sudo、完成回报、第二次重启、旧授权拒绝与保留盘检查均通过 | 其他硬件、Legacy BIOS、批量并发 |
| Ubuntu Server 22.04.4 | ISO 已核对并准备 | 完整实装 |
| Ubuntu Server 22.04.5、24.04.3、24.04.4、24.04.5 | 对应 LTS 模板已实现 | 逐 ISO 核对和完整实装 |

18 个具体版本中，目前 3 个镜像有完整实装与登录证据；不等于 3 个版本的所有兼容性问题已经关闭。CentOS 7.9 的验收版本为 alpha.3，Rocky 8.10 最新回归为 alpha.7，Ubuntu 22.04.3 为 alpha.8。更新管理端不自动等于旧版本已重测。详细证据见 [CentOS 7.9](../deployment/centos7.md)、[Rocky 8.10](../deployment/rocky8.md)和 [Ubuntu 22.04.3](../deployment/ubuntu22.md)。

## 服务端平台

| 平台 | 当前验证范围 |
| --- | --- |
| Rocky 9.8 | 现场同机部署，TLS、管理接口、iVentoy / 外部 DHCP 配合及真实 PXE 已验证 |
| Rocky 8 / 10、Debian 12 / 13、Ubuntu 24.04 / 26.04 | 静态二进制与安装脚本已提供；CI 检查用户空间启动，不等于完整 systemd / PXE 现场验收 |
| Windows 10 / 11 | Windows 二进制与安装脚本已提供；单元测试使用 Windows CI，桌面系统同机 PXE 仍待验收 |

## 当前限制和下一步

1. Ubuntu 22.04.3 的 alpha.8 任务于 `2026-10-15T09:06:50Z` 自动领取安装授权，`09:28:43Z` 首次硬盘启动后回报 `installed`。没有输入 `yes`，也没有在安装后手动选择或调整硬盘启动项；第二次正常重启通过。此结果只覆盖本次 22.04.3 live-server / UEFI / Intel X710 组合。
2. Rocky 8.10 的约 10 分钟早期网络等待修复已通过 2026-10-15 的完整实机回归，initrd 阶段为 12.733 秒。SSH、sudo、完成回报和旧授权拒绝检查通过，保留盘分区表及四项设备签名与基线一致。
3. 当前存储模板只处理按序列号指定的一块普通非 USB 磁盘；已有 LVM / RAID 需要独立的离线准备，不自动拆除跨盘存储。
4. 尚未实现经过验收的 iVentoy 自动配置适配器、批量授权与并发限制、安装失联超时诊断和更细的实时进度。
5. Windows 客户端安装为次要优先级；WinPE 组件与驱动流程尚待完整验证。fyhocsys 接入留到装机阶段完成后另行安排。

下一批应按清单逐 ISO 验收 Rocky 9 与其他 Ubuntu Server 小版本，再推进 Debian；CentOS Linux 8.5 和 Stream 9 / 10 还需补独立模板与版本保护。已通过的三项不能代替剩余 15 个版本的结果。Ubuntu 当前依赖在线软件源，本次安全更新约用时 15 分钟；尚未提供离线软件仓库方案。

服务器重启后的后台访问故障已修复：补齐指定内网和 VPN 来源的运行时、永久防火墙规则，用户已确认管理端与 iVentoy 页面恢复。现场地址分别为 `https://192.168.1.2:26080` 和 `http://192.168.1.2:26000`。

完成本轮实装后，按用户要求将现场 iVentoy 从 1.0.44 升级到 **1.0.45 免费版**。59 个镜像入口、40 个用户文件、23 项已有参数与 Ubuntu 专属任务绑定均核对保留，管理端状态未变，旧程序与配置可回退。三项完整实装结果仍基于 1.0.44，1.0.45 暂为服务与配置验证，详见[升级记录](../deployment/iventoy-upgrade.md)。

当前属于单机现场验证中的 Alpha 版本，不能把模板覆盖范围或容器启动检查视为所有系统已支持完毕。

## 代码与验证

本轮验收代码为 `1bbaa20`，CI [38709558965](https://github.com/SkyWalkerAMD-org/ventoy/actions/runs/38709558965) 的 10 个任务全部通过：Linux / Windows 测试、Linux race、安装模板解析、二进制构建和七种 Linux 用户空间启动检查。Ubuntu 还使用原始 Subiquity 5004 与真实 udev 数据验证了磁盘匹配。

alpha.8 的 Go 测试、Go vet、22 组 Linux 安装模板解析和 JavaScript 语法检查已通过。Ubuntu 注入脚本在 Linux 临时目录中执行验证：缺失或不匹配的 user-data、异常 init 标记、错误系统版本或 MAC 均不会启用无人值守入口；匹配输入会生成预期的 live systemd 配置。实机排查还修复了早期 `/dev/null` 不存在导致的注入失败，加入对应回归，并用实际 iVentoy BusyBox 与原始 initramfs 在隔离目录执行验证。原始 Subiquity 23.08.1 的 UEFI / BIOS 存储模型检查通过，UEFI 完整无人值守实装也已通过；BIOS 模型检查不等于 BIOS 实机验收。

代码在 `codex/linux-install-verification` 分支，[PR #2](https://github.com/SkyWalkerAMD-org/ventoy/pull/2) 保持草稿，叠加在 CentOS 工作 [PR #1](https://github.com/SkyWalkerAMD-org/ventoy/pull/1) 上，尚未合并到 main。本轮两项优先工作已经收口，后续继续按 18 版清单逐 ISO 验收。
