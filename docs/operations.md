# 日常装机与发布维护（alpha.49）

本文说明当前操作。旧测试记录中的 `ventoy-center/PXE-Auto` 是历史入口；现在从发行版目录选择同一个 ISO，再选择手动或自动安装脚本。

## 装机与 setup

1. BIOS/UEFI 从网卡启动，进入 iVentoy，选择发行版目录中的系统版本。
2. 不选择自动脚本时进入原安装器手动安装；选择自动脚本时进入本机安装流程。
3. 自动安装可选 ext4/XFS：红帽系默认 XFS，Ubuntu/Debian 默认 ext4。多盘、旧 LVM/软件 RAID 在本机选盘或 RAID1，核对分区容量后输入 `yes`；跨未选盘的存储仍会停止。
4. 系统正常重启到硬盘后，在带外确认登录提示、主机名和 IP，通过 SSH 实际登录。
5. 手动安装保留安装器的账户和网络；预置文件不等于已运行完整配置。以 root 执行 `./ventoy-linux-setup.run`，按提示设置主机名、可选 root 新密码，并检查 SSH、DHCP、内核/更新策略、E610 状态及中心软件结果。自动安装后同样可以运行 setup 获取 tools 最新软件。

setup 的各阶段有 `SETUP_STAGE` 标记。退出 0 表示本次请求完成；3 表示 DHCP/驱动/固件仍待处理；4 表示软件步骤失败，不能因为 RPM 数据库里有包记录就视为成功。详细日志在 `/var/log/ventoy-manual-init.log`，软件结果在 `/var/lib/ventoy-center/software/last-result.json`。`--check` 不重新安装，`--software` 单独重试软件。

已有系统无需重装即可检查、替换 setup（alpha.49 起提供）：

```bash
./ventoy-linux-setup.run --version
./ventoy-linux-setup.run --check-version
./ventoy-linux-setup.run --update-setup
./ventoy-linux-setup.run
```

替换使用原包中的中心 HTTPS 信任和软件读取凭据，验证长度及 SHA-256，备份旧文件后原子替换。下载后不会自动执行。更早版本需先从管理端下载新版一次。此更新不创建后台自动升级任务，也不自动更新 Linux 内核。

## 版本发布与回退

管理端“版本与维护”显示版本、全部入口预置版本、setup 校验值、依赖缓存与维护快照。“校验全部入口文件”执行只读内容校验；版本文字一致不能替代文件校验。

在中心 Linux 服务器以 root 运行源码包中的维护工具，配置中的路径必须是绝对路径。程序包和源码放在菜单外；所有临时配置、凭据、备份保存在 root 专属目录，不能放入 Git 或 tools。

```bash
python3 deploy/release.py stage --releases /home/ventoy-center/releases \
  --name alpha49 --candidate /path/to/new/ventoy-center --commit COMMIT_SHA
python3 deploy/release.py verify --releases /home/ventoy-center/releases --name alpha49
python3 deploy/release.py activate --releases /home/ventoy-center/releases --name alpha49
python3 deploy/release.py activate --releases /home/ventoy-center/releases --name alpha49 --apply
```

`stage` 不停止生产服务。它通过隔离管理进程生成所有已绑定入口及 setup，校验 ISO 身份、入口文件、各预置包中的 setup 字节一致性。当前仅支持已经共用 ISO、文件名及绑定不变的发布；新增或改变绑定需单独迁移。

`activate` 默认只核对；`--apply` 才发布。它检查活动任务及网络镜像连接，备份原二进制、setup、入口配置，短暂停止服务后原子替换。失败会恢复旧版本。任务记录不会被历史快照覆盖。服务健康与文件一致性属于软件验证，不代表所有硬件安装成功。

tools 中的 setup 保持 root 所有，给予管理数据所属组只读权限（0740），供管理服务核对实际文件。发布完成前还会用管理服务身份校验全部入口和 tools 文件；无法读取或内容不一致都会回退。回退恢复原来的文件权限。

发布与回退均保存并恢复 iVentoy 的网络运行状态、接口和地址参数；检查原生 PXE 状态，不能仅以 systemd 进程在线判断已恢复。原先正在服务的中心必须回到 `running`，原先主动停止的中心保持 `ready`。

```bash
python3 deploy/release.py rollback --releases /home/ventoy-center/releases --name alpha49
python3 deploy/release.py rollback --releases /home/ventoy-center/releases --name alpha49 --apply
```

只允许回退当前发布，或恢复中断于 `activating` 的发布。仍要求没有安装客户端；若备份完整性或入口清单改变则停止。磁盘损坏、断电等无法自动恢复时，保留发布目录和 transaction.json 供恢复，不要覆盖其中内容。

## 依赖预热与缓存

缓存默认最多 4 个并行下载（`installer_cache_concurrency` 可设 1–16）；同一文件合并请求。下载体开始前预留空间，失败和不完整内容不发布。容量默认 8 GiB，受限对象最大 256 MiB。APT 仍验证官方签名与包哈希；缓存不改变原始内容、不放宽过期检查。

预热在独立、可丢弃的 Linux 根目录执行，不在中心系统安装 APT 包。根目录需已有 apt-get、对应官方 keyring 和可用 DNS；需准备其正常运行所需的 `/dev`、`/proc`，并建议用私有 mount namespace。不要使用中心根目录或正在运行的目标机根目录。确认后在隔离根目录建立 `.ventoy-cache-disposable` 标记。

```bash
python3 deploy/warm-installer-cache.py --apt-root /path/to/disposable-apt-root \
  --origin ubuntu --suite jammy --keyring /usr/share/keyrings/ubuntu-archive-keyring.gpg \
  --cache-url http://192.168.1.2:26081/installer-cache \
  --output /var/lib/ventoy-center/data/cache-warm/ubuntu-jammy.json
```

该工具使用空的包状态和独立源配置下载 SSH/UEFI 引导程序及其依赖，不安装软件、不执行系统升级。Debian 使用 `--origin debian --suite bookworm` 和 Debian 官方 keyring。EL 安装仍优先使用本地 DVD。预热完成是带时间的依赖验证，不能据此承诺所有版本完全离线安装；管理页面保留各次预热结果。报告文件需要允许管理服务账户读取。

## 磁盘与备份

```bash
python3 deploy/maintenance.py --releases /home/ventoy-center/releases --report
python3 deploy/maintenance.py --releases /home/ventoy-center/releases --apply --report
```

默认只列出占用和候选。只清理新发布工具标记的版本目录，保留当前版本、最近两个版本及至少七天历史。挂载、符号链接、最后一份 ISO、未知历史构建目录均不清理。不会删除原始镜像、工具软件或机器任务记录。发布目录中的 ISO 是硬链接，目录逻辑大小不等于可释放空间。

管理端显示快照时间，以及 85%/95% 用量提示；目录占用可现场重新核对，实际文件系统余量由维护命令更新。没有默认周期删除任务。

## 验收记录

“实机验收”按系统、手动/自动、Intel/AMD/VM、单盘/硬 RAID/软件 RAID1+LVM 保存操作员确认。登记实际 setup 版本，不能用新管理端版本冒充旧安装的验收版本。通过需确认首次硬盘启动、再次重启、SSH、带外登录、DHCP、内核/更新策略、分区和 setup 八项。

安装报告会带 machine ID、安装 attempt ID、boot ID、版本和平台等观察信息；两次不同 boot ID 的报告便于核对重启。报告可关联，但不会自动变成实机验收通过。可导出 JSON；新版本缺少记录时显示待验收，历史通过单独标识。本轮实机验收由操作员执行。
