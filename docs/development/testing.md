# 开发与验证

```sh
go test ./...
go vet ./...
go test -race ./...  # 支持 race 工具链的 Linux 环境
node --check internal/center/web/app.js
```

测试覆盖管理接口认证和来源检查、敏感字段不出现在任务列表、并发单次领取、重启后的重放拒绝、取消/过期、状态顺序、连接文件领取阶段、路径边界、模板渲染与 Windows XML 格式。

版本测试遍历全部 profile 的 UEFI / BIOS 配置（Windows 11 BIOS 应拒绝），核对单次安装状态流转和安装包校验和。Desktop 的模板结构检查与完整安装验收分别记录；结构测试通过不代表桌面版自动安装成功。Linux 自动安装核对默认启动目标为 `multi-user.target`。Linux CI 会执行 Debian guard 的隔离测试：用临时假 sysfs / 设备和命令替身验证 MAC 不符、序列号不符/重复、USB、RAID、固件不符、依赖缺失及授权拒绝均不会写入 partman 分区确认。

此测试只执行授权钩子，不执行安装器、真实块设备、磁盘分区或外部网络操作。保留旧接入任务测试用于兼容性；新建 `enroll: true` 必须拒绝。

CentOS 7 guard 另有 14 个隔离用例：覆盖旧版 `lsblk` 的空序列号、内核 sysfs 回退、新版 VirtIO / SATA、跨设备重复序列号、错误 MAC / 版本 / 固件、USB / RAID 和授权失败。测试还模拟 `base64` 不可用，核对控制器 CA 完整保留，所有身份检查通过后才领取授权。

共用的 Linux guard 另覆盖 Ubuntu SATA、重复序列号、RAID 和绑定失败，验证目标校验、写入绑定、单次领取的顺序。Ubuntu 模板验证还执行内嵌 Python 绑定器，检查展开 / 包裹的 autoinstall 配置、SATA / NVMe / VirtIO 路径、非法设备名、异常存储配置及原子替换失败，确保其他配置与失败前文件内容保留。

首次启动脚本另执行 4 个隔离用例，确保系统账户 UID、缺失用户及 `nologin` shell 回传失败，只有普通可登录账户能回传 `installed`。

Linux CI 另用 Pykickstart 3.78 的 RHEL7/8/9/10 解析器检查 Kickstart，使用 PyYAML 6.0.3 解析 Ubuntu 配置，并对生成的 Linux 安装模板及内嵌脚本运行 `bash -n`。alpha.25 的导出结果为 52 份 Linux 模板和 28 份账户策略，已通过解析与语法检查。这能发现语法错误，不等于安装器运行验证或所有 Subiquity 版本的 schema 验证。本地可在安装上述两个 Python 依赖后运行 `python scripts/validate_templates.py 导出目录`。

Rocky 9 模板另在临时目录执行安装后的 NetworkManager 配置处理：只将其他网口的安装器自动生成连接（优先级 -999）改为手动启用，保留指定 MAC 的连接、自定义连接和非以太网连接。用例覆盖重复执行、指定连接缺失、配置损坏及原子替换失败；失败前不改动原文件。此检查不启停测试主机的网卡。

测试文件中的 ISO / RPM / DEB 都是字符串夹具，不可用于装机。可设置 `VENTOY_TEST_OUTPUT` 导出测试模板，仅用于语法检查，不把它们复制到生产 iVentoy。

CI 在 Linux 和 Windows 测试 Go 代码，在 Linux 运行 race 检查，输出两种平台的二进制包。另外用 Rocky 8/9/10、Debian 12/13、Ubuntu 24.04/26.04 容器检查静态二进制启动。CI 的容器步骤不能证明 PXE、硬件驱动或完整操作系统安装成功。

使用固定提交的官方 GitHub Actions。不要把 ISO、凭据、私有 fyhocsys 源码或运行状态提交到公开仓库。

同机文件准备测试使用临时假 iVentoy 目录和字符串 ISO，验证硬链接、清单先落盘、重启后重试、源镜像保留、执行期间禁止清理，以及未知文件、内容修改、镜像替换和符号链接冲突时拒绝删除。Windows 测试环境需允许创建 NTFS 硬链接；受限沙箱拒绝硬链接不等于程序不支持 Windows。
## alpha.23 手动安装配置包回归

`scripts/validate_manual_setup.py` 覆盖永久 MAC 匹配、修改过的 MAC / 未知配置保护、驱动解包的路径和符号链接拒绝，以及 E610 待注册证书状态不误报完成。Go 的手动包测试验证下载鉴权、缺包/损坏包处理、路径边界、文件摘要、无控制器令牌和无开发缓存。

真实离线包集成测试可设置 `VENTOY_E610_ARCHIVE` 为 `E610-MultiOS-6.4.5-r2.tar.gz`，再运行 `go test ./internal/center -run TestManual`。可选 `VENTOY_MANUAL_OUTPUT` 指定 tar.gz 输出路径，同时生成同名前缀的 `.run`。该包受固定摘要校验，不在仓库提交驱动二进制。

现场隔离运行验证覆盖 Ubuntu 22.04.3 / 24.04.3、Debian 10.11 / 13.4、CentOS 7.9、Rocky 8.10：完整配置、重复执行、重启自动服务和外部 root/admin 密码 SSH。记录见 `qa-alpha23-manual/*/final-reboot-evidence.log`。测试没有改动真实 `.39`、`.235` 硬盘，也没有假设虚拟网卡等于 E610 硬件。

## alpha.25 E610 镜像验证范围

E610 仓库 `pxe-media/test_media.py` 的 9 项用例检查混合压缩归档、原始 CPIO 元数据、唯一启动记录、模块目录合并，以及 EL10 dracut 符号链接的规范路径。38 个精确 ABI 模块另在匹配的原版内核中加载、核对设备 ID 并卸载。

43 个最终 ISO 分别执行原生安装器手动流程和管理端生成的 PXE 配置，共 86/86 项隔离 UEFI 安装案例通过。手动路径由测试专用答案驱动原生安装器，交付 ISO 不包含这些答案；先检查驱动已经由原生收尾钩子持久化，再运行预置 setup。两种流程均检查外部 root/admin 密码 SSH、驱动加载/卸载、CLI、更新策略及再次硬盘启动。测试采用可丢弃的 QEMU 磁盘，不能称作 86 次物理 PXE 验收。证据按最终 ISO SHA-256 匹配，重建后旧哈希的成功记录不计入新成品。

Ubuntu 24.04.3 完整安装采用普通 GA 6.8.0-71 入口。该 ISO 带 HWE 6.14.0-27 启动文件，但离线 pool 只有 GA 内核包；早期测试答案误指定 HWE 后失败，修正测试选择并重跑通过，没有改变 ISO 或驱动。该失败记录保留且不计入通过数，也不宣称此 ISO 的 HWE 目标系统已完成验收。

真实 iVentoy 的 DHCP、iPXE、ISO 解包和安装环境启动另在无外网路由的独立网络命名空间抽检。该层能发现直接加载内核/initrd 的安装测试无法覆盖的 `/init` 替换、追加 CPIO 遗漏及 BusyBox 解包问题；证据单独记录 `real-iventoy-uefi` 和对应哈希。CentOS Stream 10 另通过该真实网启到自动安装完成、控制器 installed 回报和两次硬盘启动的全流程。当前无 E610 实卡，虚拟网卡启动、模块加载成功不代表物理 E610 的链路、DHCP、收发或 Secure Boot 信任登记已经验收。逐项完成状态见[当前进度](progress.md)及 E610 仓库的 `catalog-evidence.json`。
