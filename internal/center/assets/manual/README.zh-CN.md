# 普通 ISO 手动安装后的统一配置

适用：x86_64 Linux，Ubuntu 20–26、Debian 10–13、CentOS / Rocky / AlmaLinux / RHEL 7–10。
在普通 ISO 安装完成、从硬盘进入系统后执行；不要在 live 安装器或 chroot 里运行。
这不是自动清盘脚本，不修改分区，不重新安装系统。

## 使用

从 alpha.31 起，直接运行 `./ventoy-linux-setup.run` 即完成完整系统配置、安装或升级中心默认软件组并自动检查，无需添加 `--apply`。`--apply` 保留兼容，`--software` 仅安装软件；`--offline` 跳过软件下载。查看候选使用 `--list-software`，检查结果使用 `--check-software`。已安装相同或更高版本不会重复安装或降级；后续中心替换软件包后再次执行即可，无需重新生成 setup。

新版预置流程会在手动和 PXE 自动安装结束后将本文件放到 `/root/ventoy-linux-setup.run`，以 root 或 sudo 运行（不用手动解压或另行下载）。此前已安装的旧系统也可从管理端下载本文件：

```bash
sudo /root/ventoy-linux-setup.run
# 如需指定主机名：
sudo /root/ventoy-linux-setup.run --hostname server01
```

不填写 `--hostname` 时保留安装时设置的合法主机名；默认 localhost 会改成带机器标识的名称。
启动器支持 Python 2.7、Python 3 和 EL8 自带的 platform-python，最小化系统不需要另装 tar。
也保留 tar.gz 包：解压后执行 `sudo bash ventoy-manual-setup/install.sh`。
默认配置 `admin / ventoy` 和 `root / ventoy`，允许密码 SSH 登录，并进行本机实际密码登录检查。
其他已有用户不会删除或改名。若需保留已有密码，使用 `--keep-passwords`；此时必须已有可用的 admin/root 密码，检查只验证策略，需另行从客户端测试登录。

完整包包括 E610 多系统 r2 离线模块和 Intel 6.4.5 源码。可通过 U 盘或 KVM 虚拟介质带入无网络系统。
`--offline` 禁止下载软件；SSH 服务、网络配置解析工具或匹配内核的编译依赖缺失时会明确停止，不伪报成功。

## 自动应用的内容

- 检测 E610 PCI 设备和当前内核的有效驱动。已原生支持的模块保留；否则使用精确系统/内核匹配的离线模块，写入驱动和启动镜像并保留回退备份。
- 没有离线模块时，可用原始源码和当前内核的精确头文件构建，不替换或升级内核。此路径需要匹配的依赖包；软件源没有历史头文件时会停止，不用另一内核的模块替代。
- 配置 SSH 密码登录、admin 的 sudo 权限，并验证 admin 和 root 的实际本地 SSH 登录。启用服务；现有 firewalld / UFW 启用时放行当前 SSH 端口。
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
