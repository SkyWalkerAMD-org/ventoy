# Ubuntu Server 22.04.3 无人值守实机验收

最新验收：2026-10-19，alpha.13 / iVentoy 1.0.45 Pro 已跑通通用 PXE 菜单，并验证镜像内核锁定和关闭自动更新，见本文末尾的 [alpha.13 记录](#alpha13-通用-pxe-菜单验收2026-10-19)。以下 alpha.8 章节保留当时的专属任务验收证据，其在线升级后内核不代表当前新装行为。

2026-10-15，使用 alpha.8 的 `ubuntu-22.04` 模板、iVentoy 1.0.44 / Standard / ProxyNet，在 Intel X710 / UEFI 物理机上完成无人值守安装、自动硬盘启动、控制台 / SSH / sudo 登录、完成回报及第二次重启验收。从选择专属任务镜像后，没有输入 `yes`、手动设置分区或调整安装后的启动顺序。本记录不扩展到其他 Ubuntu 小版本、Desktop 或 Legacy BIOS。

## 镜像与目标

- `ubuntu-22.04.3-live-server-amd64.iso`，2,133,391,360 字节，内部日期 `20230810`。
- SHA-256：`a4acfda10b18da50e2ec50ccaf860d7f20b389df8765611142305c0e911d16fd`，与 [Ubuntu 官方历史镜像校验文件](https://old-releases.ubuntu.com/releases/22.04.3/SHA256SUMS)一致。
- Xeon w7-2495X、64 GiB 内存，目标为按唯一序列号登记的一块 447.1 GiB SATA SSD；另一块同型号 SSD 保留。
- 管理端 `0.3.0-alpha.8`，验收代码 `1bbaa20`。每次失败后先保留日志、停止客户端、人工结单，再生成新的一次性任务，未重复使用已领取授权。

## 已定位并修复的问题

### 磁盘序列号匹配

此机器的 `lsblk SERIAL` 是磁盘短序列号，但 Subiquity 5004 的 `match.serial` 使用包含型号前缀的 udev `ID_SERIAL`。直接写入短序列号找不到磁盘，安装器随后报 `NoneType ... id`。

初始配置现在使用不存在的路径。安装前的 MAC、唯一短序列号、设备类型、存储和固件检查通过后，将确认的 `/dev/<disk>` 原子写入 `/autoinstall.yaml`，然后才领取单次授权。原始安装器与实际 udev 数据回归验证已通过，实机也已通过分区配置并写入目标盘。

此方式依据 [Subiquity 的 early-commands 与磁盘匹配规则](https://canonical-subiquity.readthedocs-hosted.com/en/latest/reference/autoinstall-reference.html)：安装器会在 early-commands 结束后重新读取配置。

### EFI 软件包下载

首次通过分区后，GeoIP 自动选择的镜像站返回 HTTP 403，`grub-efi-amd64`、`shim-signed` 等引导包无法下载，Curtin 以退出码 3 停止。客户端实测官方主仓库和安全仓库 HTTPS 均返回 200。

模板现在关闭 GeoIP 镜像替换，显式使用 `https://archive.ubuntu.com/ubuntu` 与 `https://security.ubuntu.com/ubuntu`；不关闭 TLS 或 APT 签名检查。修复后的实机已完成 EFI 包、系统与安全更新安装，并成功从硬盘启动。配置格式参照 [Curtin APT 配置](https://curtin.readthedocs.io/en/latest/topics/apt_source.html)。

## 实机结果

| 检查 | 结果 |
| --- | --- |
| 无人值守入口 | 任务专属 ISO 绑定 user-data 与注入包，默认脚本 1 / 超时 5 秒；自动开始安装，没有输入 `yes` |
| 安装介质 / 系统标识 | 安装记录为 `Ubuntu-Server 22.04.3 ... (20230810)`；`/etc/os-release` 为 Ubuntu 22.04.3 LTS |
| 内核 | 安全更新后的 `5.15.0-198-generic`，不等于 ISO 最初的软件包版本 |
| 安装目标 | 已登记 SSD 的 `sda1` 为 EFI 分区，`sda2` 为 ext4 根分区；从 UEFI `ubuntu` / `SHIMX64.EFI` 启动 |
| 自动硬盘启动 | 安装器自动重启后进入 Ubuntu；BootCurrent 为 `0000 ubuntu`，BootOrder 的 Ubuntu 位于 PXE 前，未手动干预 |
| 再次重启 | 正常重启再次直接进入 Ubuntu；boot ID 已改变，根分区、内核、SSH、sudo 与 cloud-init 再次核对通过，未再次执行安装回报服务 |
| 登录 | `deployadmin` 控制台密码登录、SSH 公钥登录、sudo 执行验收脚本均通过；root 密码锁定，SSH 密码认证关闭 |
| 网络 | LAN1 / Intel X710 按目标 MAC 配置，DHCP 和到管理端的路由正常 |
| 系统状态 | 无失败 systemd 服务；cloud-init 为 done 且无错误；`dpkg --audit` 无输出 |
| 首次启动回报 | 任务 `bca977d05cda09d2e66b7d33` 于 `2026-10-15T09:06:50Z` 自动领取授权，`09:28:43Z` 回报 `installed`；服务退出码 0，随后禁用并删除凭据脚本 |
| 重放保护 | 对已完成任务重放旧授权返回 HTTP 401 |
| 保留盘 | 首次和第二次硬盘启动后，`sfdisk -d /dev/sdb` 与 `wipefs -n /dev/sdb /dev/sdb1 /dev/sdb2 /dev/sdb3` 均与基线逐字一致；保留原有 VFAT / XFS / LVM 签名，未挂载为根目录 |
| 启动耗时 | 首次 kernel + userspace 为 13.665 秒；第二次为 8.105 秒，含固件和引导器的第二次总启动时间为 58.972 秒 |

之前的 alpha.7 实装于 2026-10-14 完成，但需要一次 `yes` 和安装后手动调整 BootOrder。此次使用新任务重新实装，验证了这两个问题的修复；没有沿用此前已安装系统作为无人值守证明。安装期间曾切换到 live shell 读取更新日志并启动仅在内存中的诊断 SSH，未输入安装答案、修改安装配置或代替安装器执行写盘。正常安装重启后，该临时 SSH 不再存在。

原始安装日志、验收 JSON、磁盘基线及旧授权拒绝结果保存在现场服务器的受限 QA 目录；不将口令、任务凭据或原始 user-data 提交到仓库。

## alpha.8 无人值守实现与回归

`ubuntu-22.04` 的新任务会同时生成 `user-data` 和 `ubuntu-iventoy.tar.gz`，两者需绑定到同一专属任务镜像。兼容包使用 [iVentoy Live Injection 入口](https://www.iventoy.com/en/doc_live_injection.html)，在 live root 启动前核对该任务 user-data 的 SHA-512、目标 MAC 和 Ubuntu 22.04 系统标识，再为 live 环境中的 Subiquity 服务添加启动参数。22.04.3 原始 initramfs 提供 `sha512sum`，未提供 `sha256sum`，因此这里采用 SHA-512。原始 ISO 与 snap 文件保持原样，磁盘、固件和单次授权检查仍在 early-commands 中执行。

2026-10-15 的早期候选实测停在安装确认提示，目标盘未写入。使用该任务实际启动的 iVentoy BusyBox、注入包和 Ubuntu 原始 initramfs，在独立临时目录中复现了原因：早期注入时 `/dev` 尚不存在，摘要校验的 `>/dev/null` 重定向失败，兼容入口未启用。已移除这一依赖，并补充缺失 `/dev` 的 Linux 正反向回归；修复后的实际启动组件隔离执行和新任务完整实装均通过。失败任务保存日志后结单，未重用其已领取授权。

安装配置还设置 Curtin `grub.reorder_uefi: false`，避免把当前 PXE 入口重新移到首位；`remove_duplicate_entries: false` 保留现有 EFI 入口，参见 [Curtin GRUB 配置](https://curtin.readthedocs.io/en/latest/topics/config.html#grub)。原始 Subiquity 23.08.1 在 guided layout 分支会忽略 `storage.grub`，因此 22.04 模板在身份和存储检查通过后，将占位布局改为仅包含已核实磁盘的显式 GPT 分区配置：UEFI 使用 1 GiB EFI 分区和 ext4 根分区，BIOS 使用 1 MiB BIOS 引导分区和 ext4 根分区。配置改写不写磁盘，安装仍需随后成功领取单次授权。旧任务的安装包不会自动改写；应创建新任务进行回归。

已通过模板解析、Linux 下的脚本正反向检查，以及原始 Subiquity 23.08.1 文件系统模型的 UEFI / BIOS 配置检查：目标盘、分区大小、启动配置及传给 Curtin 的 GRUB 选项符合预期，保留盘不在写入配置中。随后 UEFI 实机验证了无需安装确认、装完自动硬盘启动和第二次正常重启。验收代码的 [CI 38709558965](https://github.com/SkyWalkerAMD-org/ventoy/actions/runs/38709558965) 全部 10 项通过。

## 验收边界

- 需要为新任务在 iVentoy 原生页面绑定对应 user-data 与注入包，并选择正确任务镜像。管理端尚未提供经过验收的一键配置 iVentoy 适配器。
- 这次仅验收 22.04.3 live-server / Intel X710 / UEFI。兼容包暂不用于 Ubuntu 20.04 / 24.04 / 26.04；22.04.4 / 22.04.5 仍需各自完整实装，BIOS 目前只有配置模型和脚本检查。
- 当前 Ubuntu 模板需要能访问配置的软件仓库。此次系统仍标识为 22.04.3，但已安装在线安全更新；不属于完全离线或原始软件包版本固定的验收。安全更新阶段约用时 15 分钟并正常结束；`ubuntu-pro-client` 的配置文件冲突使相关 `ubuntu-advantage-tools` 更新被保留，未阻塞安装，`dpkg --audit` 无异常，不宣称所有可更新包均已升级。
- 目标盘之前的 LVM 已在获授权后离线准备；模板仍拦截已有 LVM / RAID，不自动拆解跨盘存储。
- 保留盘验收核对分区表与文件系统 / RAID 签名，未进行整盘逐字节数据校验。

## alpha.13 通用 PXE 菜单验收（2026-10-19）

在相同的 22.04.3 live-server / Intel X710 / UEFI 机器上，使用 `0.3.0-alpha.13` 和 iVentoy 1.0.45 Pro 重新实装。入口为 `ventoy-center/PXE-Auto/ubuntu-22.04.3-live-server-amd64-Auto-e0901e/image.iso`，默认自动安装脚本编号 1。选择系统版本后，在控制台选择 Q0Q 磁盘并输入完整序列号清空确认；本次没有预建本机安装任务，也没有手工填写安装器答案或调整安装后的启动顺序。

前一次 alpha.12 尝试创建任务后，在 EFI 包下载时返回 HTTP 403。现场日志确认，`early-commands` 写入的 `/autoinstall.yaml` 带 cloud-init 的 `autoinstall:` 外层，而该镜像中的 Subiquity 23.08.1 重读时直接读取顶层参数，因而丢弃了指定软件源、存储和内核设置。alpha.13 在写入前验证并去掉该外层，再执行存储绑定、内核固定和授权领取。前次失败日志已归档、任务已结单；获授权的 Q0Q 在重试前离线清除旧分区签名，Q0R 保留。

| 检查 | 本次实机结果 |
| --- | --- |
| 自动创建任务 | `8644357133c0fcd0d05b3772` 由控制台确认创建；入口 `e0901e15c7efbe038897df5b` |
| 时间 | `00:39:45Z` 领取安装授权，`00:43:53Z` 首次硬盘启动回报 `installed`，间隔约 4 分 8 秒 |
| 实际安装配置 | 安装器日志保留指定 HTTPS 官方主源 / 安全源、GeoIP 关闭及仅 Q0Q 的显式分区；Curtin 报 `Installation finished.` |
| 目标盘 | `BDD9N5327I2709Q0Q`，1 GiB FAT32 EFI 分区与约 446.1 GiB ext4 根分区；根目录为 `/dev/sda2` |
| 内核 | 配置固定 `linux-image-5.15.0-78-generic`；首次与第二次启动均为 `5.15.0-78-generic`，image / modules 包版本 `5.15.0-78.85` |
| 更新策略 | 策略自检返回 0，内核包 hold，APT 周期任务为 0，`apt-daily` / `apt-daily-upgrade` 的 service / timer 及 `unattended-upgrades.service` 均 masked / inactive |
| 自动硬盘启动 | BootCurrent 为 `0000 ubuntu`，指向 Q0Q 新 EFI 分区；Ubuntu 位于 PXE 前，保留原 CentOS EFI 指向 |
| 第二次重启 | 正常 `systemctl reboot` 后 boot ID 改变，自动进入同一 Ubuntu；根分区、内核、策略及 SSH / sudo 再次通过，未重复执行安装回报服务 |
| 系统状态 | 两次均无失败 systemd 服务；cloud-init 为 done；`dpkg --audit` 无输出 |
| 授权和回报 | 回报服务退出码 0，之后 disabled / inactive；服务端任务为 installed，任务凭据已清空 |
| Q0R 保留盘 | 两次启动后首尾各 1 MiB SHA-256 与基线一致，分区边界、分区 UUID 和 VFAT / XFS / LVM 布局保持；未作为根目录或 EFI 挂载 |
| 启动耗时 | 首次 kernel + userspace 为 13.387 秒，第二次为 13.470 秒；第二次含固件和引导器总计 64.378 秒 |

完整安装日志及两次启动验收 JSON 保存在工作区私有 `.local` 目录，任务验收摘要保存在现场服务器 `/home/ventoy-center/pxe-menu-alpha13/acceptance-state.json`。原始日志可能含任务配置，不提交到仓库。此结果只覆盖本次 Ubuntu 22.04.3 / UEFI / X710；不代替其他小版本、其他硬件或并发测试。内核保持镜像版本不表示所有引导依赖均来自离线 ISO，安装仍可访问上述 HTTPS 软件仓库。保留盘检查不是整盘逐字节校验。

## alpha.16 E610 PXE 自动安装验收（2026-10-19）

在 Intel E610（PCI `8086:57b0`）和 MR9540-8i 逻辑盘的另一台实机上，使用 Ubuntu Server 22.04.3 GA 适配镜像完成通用 PXE 实装。镜像保留 `5.15.0-78-generic` 内核，加入匹配 ABI 的 Intel ixgbe 6.4.5，并将驱动、initramfs hook 和更新策略保留到目标系统。构建代码及脱敏证据位于独立 [E610 仓库的 Ubuntu 目录](https://github.com/SkyWalkerAMD-org/E610/tree/codex/ubuntu-22.04.3-e610/ubuntu22-e610)。

初次控制台选盘超时后没有写盘。随后核对已授权逻辑盘的 WWN 与 DMI，单独清理原 CentOS LVM；没有解除通用 PXE 对旧 LVM/RAID 的保护。重新启动安装服务，经正常选盘和完整序列号清盘确认后，任务 `8f866192ef23c5694adb87de` 于 `05:35:43Z` 自动领取授权，`05:41:01Z` 首次本地启动回报 installed。安装后无需手工部署驱动或调整启动顺序。

- 两次本地 UEFI 启动的 boot ID 不同，根分区 `/dev/sda2` 为 ext4，EFI 分区 `/dev/sda1` 为 FAT32；BootCurrent 指向本地 Ubuntu。
- 两次均使用 E610 获取 DHCP IPv4，SSH / sudo 正常，经 E610 到局域网服务器 ping 均为 4 发 4 收；其他业务网口没有 IPv4。
- ixgbe 为 6.4.5；目标模块及解包后的 initramfs 模块 SHA-256 均为 `d1f954e9dde92c40d76b5244546ed22922613202b6fd92ea3dc9be9e28944586`。
- 两次均运行 `5.15.0-78-generic`，已安装的 image / modules 包为 `5.15.0-78.85` 并 hold；APT pin 拒绝新内核 ABI，五个 APT 自动刷新/无人值守升级 service/timer 均 masked、inactive。
- 首启 E610 验证服务成功；管理端任务 installed，回报脚本删除，回报服务 disabled；两次均无失败 systemd 单元。

适配 ISO 的 SHA-256 为 `a8a8ccda7c3fa9f7643ac09418375f479c3cf1eaccafd2603f54968d8f6bcb80`。此结果只增加 22.04.3 的 E610 硬件组合，不增加已验收系统版本数量；不覆盖 Desktop、HWE、Secure Boot、其他 ABI、高速吞吐或其他 Ubuntu 小版本。手动安装共享层已通过镜像及目标 initramfs 检查，未将全流程手动实装记为通过。

## alpha.19 单内置盘自动安装（2026-10-23）

Ubuntu PXE 流程改为选择系统和自动安装脚本后直接安装。仅一块可用内置盘时自动绑定序列号，无控制台选盘或 `yes` 输入；多盘时停止，管理端预先指定并授权目标盘后才可继续。USB / 可移除盘排除，未知连接及不可用的其他内置盘不会被忽略；原有 LVM / RAID 保护保留。

现场的 22.04.3 原版、22.04.3 E610 和 24.04.3 三个入口已更新、重新生成注入包并刷新 iVentoy，脚本菜单取消自动倒计时。管理员 `admin`、管理员/root 初始密码、镜像内核固定、APT 周期任务关闭及无人值守升级屏蔽沿用原配置。已有安装结果和任务历史保持。

Windows / Linux Go 测试、安装配置解析、无键盘输入客户端流程、多盘停止和预指定任务复用检查通过。本轮未执行新的物理重装，不能将本次流程更新记为 24.04.3 或其他 Ubuntu 小版本的完整实装通过。部署记录见服务器 `/home/ventoy-center/qa-alpha19/final-status.json`，操作步骤见 [PXE 菜单安装](pxe-menu.md)。

## alpha.22 本机多盘选择与 root SSH 实装验收（2026-10-23）

`.235` 经 LAN1 / X710 / UEFI 进入 iVentoy 1.0.45 Pro，选择 `PXE-Auto` 下的 Ubuntu 22.04.3 原版对应入口及 `user-data`。安装器在本机列出两块盘；选择 Q0Q 并输入 `yes` 后自动执行，未到后台指定盘或预建安装任务。任务 `e9e59c6742e87433ac5978e9` 回报 `installed`。安装后的第一次重启曾显示 `/cdrom` 卸载失败，随后从硬盘启动；没有强制断电或改动启动顺序。

两次硬盘启动均通过以下检查：

- `root / ventoy` 与 `admin / ventoy` 均以密码通过 SSH 登录，root SSH 生效配置检查通过。
- 主机名 `VC-C7-235`；`eno2`（MAC `b0:82:e2:30:51:a2`）取得 DHCP 地址 `192.168.1.52`。
- 根分区为 Q0Q 的 `/dev/sda2`；Q0R 只有整盘设备，没有分区或文件系统签名。
- 默认 `multi-user.target`，无失败服务；内核保持 `5.15.0-78-generic`。
- 内核策略检查成功，APT 五项周期设置为 0，五个更新服务/计时器均 masked / inactive。
- `safe-server-init` 已预置，hostname 及网络预览成功；网络预览保留已联网接口，为其他三个未配置网口生成方案并通过隔离 Netplan 校验，没有实际应用这些新网口配置。

两次 boot ID 分别为 `8a9034ab-fdd3-4f54-8166-fb9269f50ff1`、`38b3cae4-0419-49f2-a347-08b028926d03`。证据位于服务器 `qa-alpha22/physical-boot1.json`、`physical-boot2.json` 及本地 `.local/alpha22-acceptance/`。普通 ISO 手动安装未参与此回归，不会自动获得上述全部配置；其他系统和硬件的状态见[进度记录](../development/progress.md)。
