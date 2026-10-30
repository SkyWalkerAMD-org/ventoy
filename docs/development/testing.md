# 开发与验证

alpha.46 增加独立 P95 助手与 CentOS 7.9 适配；现场发布和验收范围见[当前进度](progress.md)，接入私有配置见[软件与节点接入](../deployment/node-software.md)。包副本的真实 APT/RPM 回归只运行于带 disposable 标记的可丢弃根目录；若同一环境已经设置内核策略，`validate_package_retention.py` 必须通过 `--policy` 提供对应的真实策略检查脚本，仍在安装后验证内核锁定。

## P95 助手与旧 systemd

`tools/p95-priority-helper/src` 是独立 Go 模块，需在该目录执行 `go test ./...` 和 `go vet ./...`；根模块测试验证 setup 内嵌脚本的 SHA-256 固定值。`scripts/validate_p95_priority.py` 覆盖软件未更新时补装、失败重试、只读检查和平台范围；Linux 上 `scripts/validate_p95_units.py` 比较 systemd 219 与 235+ 的实际 unit 渲染，219 不使用会删除撤销日志的 RuntimeDirectory。

离线脚本可通过 `python3 tools/p95-priority-helper/build-release.py --output /tmp/p95-release` 重建，需 Go、Bash 与 Python 3。将生成脚本同步到 `internal/center/assets/manual/p95-priority-setup.sh` 后，必须同步 `p95_priority.py` 的摘要；重新生成所有入口预置包。源代码自身包含在脚本内，目标机无需编译。

目标机安装后运行 `p95-priority-fyhocsys selfcheck`、离线脚本 `--check`，确认服务 enabled/active；重复安装检查 MainPID 不变，重启助手检查运行目录保留，节点服务 MainPID 与内核不变。这里不启动硬件压测，真实混合负载的 nice 与 CPU 时间采样需另行执行，不能把空采样当成通过。

## 长期接入与安装包保留（2026-10-29，未发布）

`TestNodeJoin*` 检查独立鉴权、私有文件权限、路径/目录别名隔离、两种程序的输入格式、轮换及凭据不进入清单或 setup。`scripts/validate_node_join.py` 使用真实子进程和临时密钥文件验证两种节点命令、重复执行、旧版本拒绝、错误与超时清理、日志不含密钥、失败重试，以及连接/worker/服务失败不误报成功；root 环境另生成真实 X.509 证书核对持久连接的指纹、主机名和 `0600` 权限。

`scripts/validate_software.py` 增加 DEB/RPM 的副本保留、旧结果补传、失败后复用与用户改动保护。`scripts/validate_package_retention.py` 只允许在有 `/run/ventoy-policy-disposable-root` 标记的隔离 root 中运行，通过本地 HTTP 服务下载真实夹具包，再调用目标系统的 APT/RPM 安装；验证安装后原文件、重复执行、缺失补传且不重装、改动保护。CI 在现有原生包管理器容器内运行该检查。

本轮云端用 Ubuntu 22.04 和 Rocky Linux 9 验证真实 DEB/RPM 流程。接入测试的中心响应、节点程序和 systemd 状态是隔离夹具；不代表已接入用户生产中心、收到真实心跳、完成开机或压测验收。上游接口依据两个仓库的 0.8.6 源码；当前环境无法连到用户的内网中心。

```sh
go test ./...
go vet ./...
go test -race ./...  # 支持 race 工具链的 Linux 环境
node --check internal/center/web/app.js
```

测试覆盖管理接口认证和来源检查、敏感字段不出现在任务列表、并发单次领取、重启后的重放拒绝、取消/过期、状态顺序、连接文件领取阶段、路径边界、模板渲染与 Windows XML 格式。

版本测试遍历全部 profile 的 UEFI / BIOS 配置（Windows 11 BIOS 应拒绝），核对单次安装状态流转和安装包校验和。Desktop 的模板结构检查与完整安装验收分别记录；结构测试通过不代表桌面版自动安装成功。Linux 自动安装核对默认启动目标为 `multi-user.target`。Linux CI 会执行 Debian guard 的隔离测试：用临时假 sysfs / 设备和命令替身验证 MAC 不符、序列号不符/重复、USB、RAID、固件不符、依赖缺失及授权拒绝均不会写入 partman 分区确认。

alpha.33 增加 `TestCapacityTierLayout` 和 `scripts/validate_storage_layouts.py`：使用假 sysfs 执行实际容量函数及分区配方生成，覆盖各标称容量、档位边界、过小磁盘、不同容量的 RAID1 成员和拒绝重新绑定。导出后核对 660 个容量/模板/固件组合的固定分区及 home 余量；Kickstart 使用对应原生解析器，Subiquity 执行实际 YAML 绑定器，Debian 核对 MiB 到十进制 MB 的转换。此步骤不调用分区工具或安装授权接口。

账户策略遍历所有 Linux profile，确认 `user` 和 root 使用同一已配置密码、root 密码未锁定及 SSH 密码策略已生成。手动 setup 回归覆盖旧 admin 完成标记迁移、新版重复执行不重置密码、保留密码选项，以及系统 UID 保护。实际控制台与 SSH 登录、安装后卷大小和正常重启另以隔离完整安装及物理节点证据记录。

此测试只执行授权钩子，不执行安装器、真实块设备、磁盘分区或外部网络操作。保留旧接入任务测试用于兼容性；新建 `enroll: true` 必须拒绝。

`scripts/validate_manual_network.py` 执行真实 setup 网络包装脚本，隔离模拟 networkd / NetworkManager 的 12 个场景，检查迟到链路时先加载持久配置、失败状态传播与重试、已有主接口保留，以及有地址后重复执行不重新激活。已由 `validate_manual_setup.py` 自动运行；此隔离检查不修改测试宿主的网卡。真实 Ubuntu 虚拟机租约与重启证据另记于进度页。

### 云端旧存储重装检查

`scripts/validate_raid_guard.py` 对单盘重建和双盘 RAID1 各执行 39 个隔离场景，覆盖同名但 UUID 不同的独立卷组、相同 UUID 的克隆卷组、目标卷组名称冲突、缺失身份/成员、LVM/MD 工具失败、活动 holder、挂载和 swap。卷组范围按 UUID 判断；单纯同名不扩大清盘范围，真正跨盘的成员仍必须全部选中。实际渲染的领取授权及后续准备步骤另检查授权失败不写、授权成功才按 UUID 改名、存储变化中止、临时名称冲突和改名失败。swap 预检不改变启用状态；只有成功领取才关闭所选块设备上的 swap。用例覆盖关闭失败、关闭命令返回成功但 swap 仍启用、领取期间出现新挂载/成员/设备身份变化，以及同时需要关闭 swap 和改名时的执行顺序。Python 和原生 Debian 客户端另验证 swap 进入确认选盘、取消不下发任务，挂载/只读状态不会被 swap 状态覆盖。

`scripts/validate_storage_vm.py` 在 QEMU TCG 中运行实际生成的存储预检脚本，创建临时虚拟磁盘和真实 LVM/MD 元数据。需要 Debian/Ubuntu 的 `qemu-system-x86`、`linux-image-amd64`（或对应 Ubuntu 内核及模块）、`initramfs-tools`、`busybox-static`、`lvm2`、`mdadm`、`udev`、`fdisk`、`e2fsprogs`。在安装了这些依赖的隔离环境中运行：

```sh
VENTOY_TEST_OUTPUT=/tmp/ventoy-fixtures go test ./internal/center -run 'Test(SingleRebuildInstallerFiles|RAID)'
python3 scripts/validate_storage_vm.py /tmp/ventoy-fixtures \
  --kernel /boot/vmlinuz-实际版本 --log /tmp/ventoy-storage-vm.log
```

默认执行 27 项真实块设备检查，含所选旧卷组改名及重复准备、分区/LVM/MD 上的 swap 释放，以及未选 swap 与同名 swap 文件保持启用；可用 `--baseline 旧版导出目录` 增加一项旧版同名卷组误拦截的对照。每项检查均校验未选虚拟磁盘的完整 SHA-256。宿主只创建临时稀疏文件，无需 KVM，不映射宿主块设备，不给虚拟机配置网络。不要直接运行 `storage_vm_guest.sh`，它是测试 initramfs 的入口。检查通过只能证明存储预检与准备行为；原生安装器完整安装、硬盘重启、物理硬件和 iVentoy 网启应分别记录。

Ubuntu 重启收尾由 `scripts/validate_ubuntu_reboot.py` 检查：只有 Ubuntu Casper、overlay 根文件系统、精确挂载于 `/cdrom` 的只读 ISO 才写入运行时卸载配置；已安装系统、其他安装器、缺失挂载和可写介质均不修改。另检查重复执行、daemon-reload 失败、配置回读失败和符号链接保护。模板验证保证该步骤位于目标系统配置成功之后，仍使用安装器正常 `shutdown: reboot`，首次硬盘启动验证前不报告 installed。

alpha.26 使用 Ubuntu Server 24.04.3 的同一成品 ISO 做无硬盘 QEMU 对照：原始正常重启复现 `/cdrom` 卸载失败及随后介质弹出造成的 squashfs I/O 错误；应用运行时 `LazyUnmount=yes` 后两项消失，正常进入内核重启。两组均保留 Casper 原有的自动安装免按键标记，没有强制重启。根文件系统仍引用的部分 loop 设备会在最终关机阶段提示 busy，这不阻止重启；不据此声称所有关机警告均已消失。物理 PXE 连续重启结果单独记录在进度页。

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
