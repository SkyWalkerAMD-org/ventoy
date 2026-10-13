# Ventoy Center

局域网批量装机管理端。客户端在 BIOS / UEFI 的 Boot 菜单选择网卡，由 **iVentoy / PXE** 加载镜像和自动安装配置；本项目负责目标机器、安装任务、授权、安装包和进度回传。

**当前：0.1.0-alpha.1，适配与验证阶段。** 服务端和安装包生成已实现；iVentoy 规则仍通过其原生界面配置。没有进行真实硬件 PXE 安装验收，不应作为已验收的无人值守生产装机系统使用。

## 已实现

- 中文管理页面、管理口令认证、机器登记、ISO / 节点包目录扫描。
- 按 MAC、磁盘序列号、启动模式绑定安装任务；草稿需核对机器名和磁盘后授权。
- 24 小时任务凭据、单次安装领取、持久状态、取消与过期检查；服务重启不自动重发安装。
- 生成 Rocky Linux 9 Kickstart、Ubuntu Server 24.04 autoinstall、Windows 11 / Server 2022 安装文件。
- Linux 首次启动安装本地提供的 fyhocsys-node 包、核验 SHA-256、导入专属连接文件并检查本地服务。
- Windows 安装前核对 MAC、磁盘序列号、磁盘编号和固件模式；需提前制作含所需可选组件的 WinPE。

## 当前边界

- **Windows 尚不能自动接入 fyhocsys。** 当前 fyhocsys 只有 Linux 节点包，本项目不会把 Windows 的安装回传标成压测节点已接入。Windows 节点和监控能力需在 fyhocsys 侧另外实现。
- `enrolled` 表示 Linux 节点连接文件已导入且服务通过本地检查；中心实际连通仍需在 fyhocsys 核对新心跳。sckocp 的安装与原有授权另行准备。
- **安装包下载不是 PXE 下发。** 需要在 iVentoy 中关联镜像、脚本及按 MAC 的分机独启规则。尚无经过现场验证的 iVentoy 自动配置适配器。
- 每台机器生成独立任务。多台同时使用同一个源 ISO 时，配置不同 ISO 路径（可用硬链接），避免一个镜像的全局默认脚本串到其他机器。
- Windows ISO 的 WinPE 默认通常不含 PowerShell / StorageWMI，组件不足时安装保护会阻止继续。当前 Windows 模板语言支持 zh-CN / en-US，须与镜像一致。

## 服务端平台

| 平台 | 交付方式 | 当前验证范围 |
| --- | --- | --- |
| Rocky Linux 9（首要目标，包含用户的 9.8 环境） | 静态 Linux x86-64 二进制 + systemd | 单元测试、CI 启动检查；9.8 实机共机验收待做 |
| Rocky Linux 8 / 10 | 同一 Linux 二进制 | CI 启动检查，不等于 systemd / PXE 实机验收 |
| Debian 12 / 13 | 同一 Linux 二进制 + systemd | 同上 |
| Ubuntu 24.04 / 26.04 | 同一 Linux 二进制 + systemd | 同上；其他 24–26 间的短周期版本未单独验证 |
| Windows 10 / 11 x64 | Windows 二进制 + 可选计划任务 | Windows 单元测试；桌面系统与 iVentoy 共存需实机验收 |

这里列的是**装机服务器的运行系统**，不是自动安装模板的支持列表。首批安装目标为 Windows 11 / Server 2022、Rocky Linux 9、Ubuntu Server 24.04。

## 本地启动

需要 Go 1.25 或更新版本；运行已编译程序不需要 Go、Node.js 或 Python。

```sh
go build -o ventoy-center ./cmd/ventoy-center
./ventoy-center init --dir ./runtime
./ventoy-center serve --config ./runtime/config.json
```

打开 `http://127.0.0.1:26080`，使用 `runtime/admin.token` 登录。将 ISO 和 fyhocsys 节点包分别放到 `runtime/iso/`、`runtime/packages/`。

默认只监听回环地址。给局域网客户端使用之前，需要配置可达的 HTTPS 地址与证书；详见 [Rocky 9 部署](docs/deployment/rocky9.md)。Windows 命令将可执行文件改为 `ventoy-center.exe`。

## 文档

- [Rocky 9 与 fyhocsys 同机部署](docs/deployment/rocky9.md)
- [外部 DHCP、iVentoy 和安装包配置](docs/deployment/pxe.md)
- [Windows 与其他 Linux 服务端](docs/deployment/platforms.md)
- [架构、任务状态与尚未完成的能力](docs/development/architecture.md)
- [开发与验证](docs/development/testing.md)

本项目是独立编写的管理端，不包含 iVentoy、Windows ISO、fyhocsys 私有源码、节点包或任何授权文件。iVentoy 是单独安装和许可的软件；按 MAC 分机独启及商用需参考其[专业版说明](https://www.iventoy.com/cn/doc_edition.html)。
