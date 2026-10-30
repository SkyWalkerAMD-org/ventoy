# 普通 ISO 手动安装后的统一配置

适用：x86_64 Linux，Ubuntu 20–26、Debian 10–13、CentOS / Rocky / AlmaLinux / RHEL 7–10。
在普通 ISO 安装完成、从硬盘进入系统后执行；不要在 live 安装器或 chroot 里运行。
这不是自动清盘脚本，不修改分区，不重新安装系统。

## 使用

P95 1.0.2 集成会在软件安装及已配置的节点接入完成后，为已安装的 fyhocsys-node / sckocstp-node 启用独立优先级助手。助手每秒将受管 P95 默认的 nice 19 工作线程调到 nice 0，源码与说明保留在 `/usr/local/share/p95-priority-helper/1.0.2/`，不修改节点项目或包内文件。软件包版本未变时也会检查助手；`--check-software` 只读核验助手，安装/核验失败沿用软件步骤失败与重试逻辑。支持 CentOS 7.9 的 systemd 219 和 EL7 工具目录；低于 systemd 219 及非 x86_64 明确跳过，其他安装错误仍报失败。已有系统需更新旧 setup 才包含此功能；也可单独执行 `bash p95-priority-setup.sh`。助手不启动压测，实际并发效果需在运行混合测试时检查两类程序的 CPU 时间。

从 alpha.31 起，直接运行 `./ventoy-linux-setup.run` 即完成完整系统配置、安装或升级中心默认软件组并自动检查，无需添加 `--apply`。`--apply` 保留兼容，`--software` 仅安装软件；`--offline` 跳过软件下载。查看候选使用 `--list-software`，检查结果使用 `--check-software`。已安装相同或更高版本不会重复安装或降级；后续中心替换软件包后再次执行即可，无需重新生成 setup。

匹配的软件包原文件同时保存在 `/root/ventoy-tools/`，以原名和 `0600` 权限保留，安装失败也可复用。软件已安装但包副本缺失时会补传；再次执行校验摘要后跳过下载，用户改动的同名文件会保留并报错。`--check-software` 同时检查这些副本。

中心配置长期接入密钥后，setup 在节点包安装成功后调用官方 `sckocstp-node` / `fyhocsys-node join --key-file`，检查连接配置、证书指纹、worker 和开机服务。中心和节点均需 0.8.6 或更新；节点名采用本机主机名，资产编号按原生 DMI 规则。重复执行保留相同中心的绑定；接入失败保留完整 setup 的重试请求。长期密钥不写入日志或安装包，临时密钥文件用后删除，日常开机由节点自身服务连接中心。该功能与 FYHOC 开机解锁独立。

绑定前先输入/确认唯一主机名，再应用并核对运行值和 `/etc/hostname`。改名未完成、两者不一致或首次接入仍是 `ubuntu` 等常见默认名时不会绑定。开机恢复也先完成主机名请求。需要指定名称用完整 `setup --hostname NAME`，`--software` 只用已有名称，不能同时带 `--hostname`。

新版预置流程会在手动和 PXE 自动安装结束后将本文件放到 `/root/ventoy-linux-setup.run`，以 root 或 sudo 运行（不用手动解压或另行下载）。此前已安装的旧系统也可从管理端下载本文件：

```bash
sudo /root/ventoy-linux-setup.run
# 如需指定主机名：
sudo /root/ventoy-linux-setup.run --hostname server01
```

直接在终端运行时会先显示当前主机名，输入新名称即可设置，直接回车则保留当前持久化主机名。也可用 `--hostname NAME` 跳过提问。非交互运行和开机恢复不提问；未指定名称时保留当前名称，默认 localhost 会改成带机器标识的名称。显式名称只应用一次，不会在以后运行 setup 或重启时恢复旧名称。

完整 `.run` 配置结束后，可交互修改 root 密码：输入新密码并再次确认，输入不显示；直接回车保留当前密码。修改后会用新密码实际验证 root SSH 登录，普通账户 `user` 的密码不变。临时验证文件仅 root 可读，验证后删除；不把新密码写入日志或保存供开机使用。后续运行 setup、检查或重启保留自定义 root 密码；因未保存明文，后续检查仅验证 root 的账户与 SSH 配置，密码实际登录验证在修改当次完成。非交互、开机恢复和软件单独安装不询问密码。

装好后可执行 `sudo hostnamectl set-hostname server02`；如需同时同步本机 hosts，使用 `sudo safe-server-init hostname server02 --update-hosts`。旧版 setup 曾保存固定主机名，已安装的机器需要替换为新版 `.run` 并运行一次，才能更新开机恢复服务；已自行修改密码时加 `--keep-passwords`。
启动器支持 Python 2.7、Python 3 和 EL8 自带的 platform-python，最小化系统不需要另装 tar。
也保留 tar.gz 包：解压后执行 `sudo bash ventoy-manual-setup/install.sh`。
默认配置 `user / ventoy` 和 `root / ventoy`，允许密码 SSH 登录，并进行本机实际密码登录检查。
其他已有用户不会删除或改名。若需保留已有密码，使用 `--keep-passwords`；此时必须已有可用的 user/root 密码，检查只验证策略，需另行从客户端测试登录。

完整包包括 E610 多系统 r2 离线模块和 Intel 6.4.5 源码。可通过 U 盘或 KVM 虚拟介质带入无网络系统。
`--offline` 禁止下载软件；SSH 服务、网络配置解析工具或匹配内核的编译依赖缺失时会明确停止，不伪报成功。

## 自动应用的内容

- 检测 E610 PCI 设备和当前内核的有效驱动。已原生支持的模块保留；否则使用精确系统/内核匹配的离线模块，写入驱动和启动镜像并保留回退备份。
- 没有离线模块时，可用原始源码和当前内核的精确头文件构建，不替换或升级内核。此路径需要匹配的依赖包；软件源没有历史头文件时会停止，不用另一内核的模块替代。
- 配置 SSH 密码登录、user 的 sudo 权限，并验证 user 和 root 的实际本地 SSH 登录。启用服务；现有 firewalld / UFW 启用时放行当前 SSH 端口。
- 配置和检查主机名。对无配置的硬件有线网卡创建 DHCP；自动激活本次创建的配置，检查有连接网卡的 IPv4 地址。已有 DHCP/静态地址、Bond/VLAN/Bridge、USB、Wi-Fi 和现有 SSH 接口保留。
- 对新增次要网卡抑制默认路由和 DNS 接管。支持 NetworkManager、Netplan、systemd-networkd；ifupdown 支持 ISC dhclient 和 Debian 13 的 dhcpcd，旧 ifcfg 支持经过确认的 ISC dhclient hook。其他复杂后端明确提示待处理。
- 固定执行时已安装的内核，关闭 APT 周期刷新 / unattended-upgrades，或 yum-cron / dnf 自动升级与缓存刷新服务。检查持久配置和当前运行状态。人工执行包管理命令仍可用。
- 设置默认进入 CLI，并安装每次启动自动继续与检查的服务。

## 结果与重启

出现 `VERIFIED` 才表示本次所有适用检查通过。`E610_NOT_PRESENT` 表示没有 E610 硬件，未替换网卡驱动。
`PENDING` / 退出码 3 表示仍需重启、MOK 证书注册或等待 DHCP 租约；不代表安装或验收完成。
脚本不会卸载正在工作的网卡驱动，也不自动重启机器。E610 新驱动需要重启时，在维护窗口重启；已安装的服务会继续配置新出现的网卡并检查。
Secure Boot 首次使用外部模块时，可能需要在固件 MOK 页面注册对应证书；这一步不能由普通安装后脚本替用户完成。

```bash
sudo bash /opt/ventoy-manual/current/install.sh --check
sudo cat /var/lib/ventoy-center/manual-init/status
sudo journalctl -u ventoy-manual-init.service
```

详细日志：`/var/log/ventoy-manual-init.log`。
配置/账户备份：`/var/lib/ventoy-center/manual-init/`（仅 root 可读）。
主机名备份：`/var/backups/safe-server-init/`。驱动回退按包内 `e610/INSTALL.zh-CN.md`；源码构建的安装器位于 `/var/lib/e610-ixgbe/manual-系统-内核/payload/install.sh --rollback`。
手动安装中已经更新过的内核不会降回 ISO 版本；原版 ISO 的安装器本身也不会因下载此包而自动获得 E610 驱动。若安装器阶段没有网卡，使用已适配的 E610 ISO 或安装器驱动注入入口。
## 当前终端的主机名显示

直接运行 `.run` 并完成系统配置检查后，若本次修改了主机名，前台终端会自动进入新的交互 Bash，立即显示新主机名，保留当前目录。通过 sudo 运行时使用原调用账户。系统主机名实际由 hostnamectl 设置并检查，刷新只处理 Bash 提示符缓存。

附加网卡/驱动等待完成（退出码 3）或软件安装失败（退出码 4）时，仍显示可选的 root 改密码提示，并刷新本次改名的终端；错误状态不会变成成功。密码留空保持原值。新 shell 退出后会回到等待 setup 的原 shell，并返回原有的失败/待完成退出码。后台、重定向输出、只读检查、软件模式或系统配置失败时不打开 shell。直接运行解压后的 `install.sh` 时，原终端可执行 `exec bash` 或重新登录刷新提示符。
