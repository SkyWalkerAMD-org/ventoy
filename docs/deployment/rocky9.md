# Rocky Linux 9：与 fyhocsys 共机部署

目标为现有 Rocky Linux 9.8 管理服务器。以下步骤安装新服务，不重装该服务器。先在独立测试客户端验收；真正被清盘的是任务中指定的客户端磁盘。

## 1. 安装管理端

使用 CI 的 `ventoy-center-packages` 产物，核对 `SHA256SUMS`，解压 Linux 包，在解压目录执行：

```sh
sudo sh deploy/install-linux.sh ./ventoy-center
```

脚本创建独立 `ventoy-center` 用户和 systemd 服务，保留已有配置。首次安装后配置位于 `/var/lib/ventoy-center/config.json`，管理口令位于同目录 `admin.token`。脚本准备服务但不自动启动；初始化的监听地址只允许本机访问。

## 2. 配置地址和 TLS

将服务证书、私钥和给安装环境使用的 CA 证书放在该用户可读的受限目录，例如 `/var/lib/ventoy-center/tls/`。证书 SAN 必须包含下面 `public_url` 使用的 DNS 名或 IP。

配置示例（全部地址、名称是示例，需替换）：

```json
{
  "listen": "0.0.0.0:26080",
  "public_url": "https://deploy.example.internal:26080",
  "data_dir": "/var/lib/ventoy-center/data",
  "iso_dir": "/var/lib/ventoy-center/iso",
  "package_dir": "/var/lib/ventoy-center/packages",
  "admin_token_file": "/var/lib/ventoy-center/admin.token",
  "tls_cert_file": "/var/lib/ventoy-center/tls/server.crt",
  "tls_key_file": "/var/lib/ventoy-center/tls/server.key",
  "bootstrap_ca_file": "/var/lib/ventoy-center/tls/ca.crt",
  "iventoy_url": "http://deploy.example.internal:26000",
  "fyhocsys_url": "https://center.example.internal"
}
```

CA 文件仅放公有证书。客户端安装环境能解析该域名、校准时间并访问此 HTTPS 地址；使用 IP 时证书需要 IP SAN。不要使用跳过证书验证的参数。Windows 的初始化下载会把配置的 CA 导入本机信任根，因此这里只能指定你的部署 CA。

新管理端用 TCP 26080，现有 fyhocsys 继续使用 TCP 443。iVentoy 仍使用自己的端口。防火墙仅允许实际的装机和管理网段，不关闭现有防火墙，不更改 fyhocsys 允许网段。

```sh
sudo systemctl enable --now ventoy-center
sudo systemctl status ventoy-center
sudo journalctl -u ventoy-center -n 50 --no-pager
```

使用浏览器登录 `public_url`。状态数据库是 `data/state.json`，其中有安装时限内凭据与配置秘密；备份整个私有运行目录，不放进 Git。当前只能运行一个管理端实例。

## 3. 镜像和 iVentoy

1. 把合法持有的 ISO 放到 `iso_dir`。目录和文件名按 iVentoy 要求使用无空格的 ASCII 名称。
2. 单独安装 iVentoy Linux x86-64，专业版授权保存在其 `data` 目录。不要把该目录纳入本项目仓库。
3. 在 iVentoy 的 `iso` 目录建立到镜像的链接，或把管理端的 `iso_dir` 指向 iVentoy 的实际镜像目录。确保两项服务有对应读取权限。当前管理端扫描不递归进入符号链接目录，宜直接配置真实目录。
4. 继续按 [PXE 配置](pxe.md) 配合已有 DHCP，先使用正常手工安装镜像验证网卡能引导。

## 4. fyhocsys 自动接入准备

每台客户端先在 fyhocsys 中登记一次。用户名、资产号、BMC 配置继续由 fyhocsys 管理。也可在这台中心本机使用已安装的 CLI：

```sh
sudo fyhocsys-center node-add --node BUILD-001 --serial ASSET-EXAMPLE-001 --keep-on --output /root/BUILD-001.fyhocsys.json
```

如果已经登记，使用首次生成的连接文件，不重复登记。新系统 hostname 必须与该节点名一致。将对应发行版的 `fyhocsys-node` RPM / DEB 放进 `package_dir`；从管理界面创建 Linux 安装任务时选中“安装后自动配置 fyhocsys 接入”，选择节点包并导入专属连接文件。

连接文件只保存在管理端私有状态中。安装保护获得单次授权后，Linux 首次启动进入 `enrolling`，才允许用该任务凭据下载对应连接文件和包。下载的包与创建任务时计算的 SHA-256 不一致会失败。包管理器仍需要可用的依赖源；无互联网环境应先提供内网软件仓库或完整依赖。

导入后运行 `fyhocsys-node check` 和本地服务检查，成功状态为“接入已配置”。最后到 fyhocsys 确认新的心跳。原 sckocp 的安装、激活和压测授权需沿用已有流程；导入连接文件不意味着工具已经激活或可以立即压测。

## 5. 验收顺序

1. 先验证管理端登录与镜像列表，再验证单台客户端 PXE 进入镜像菜单。
2. 用已备份且允许清空的测试机，核对 MAC、磁盘序列号与固件模式。
3. 新建并授权任务，导入安装包与按 MAC 启动规则，从客户端 Boot 菜单选择网卡。
4. 检查只清空指定盘、安装成功、节点连接文件正确导入、fyhocsys 收到新心跳。
5. 删除本次 MAC 启动规则和 ISO 别名；再验证旧安装包重放被拒绝。
6. 通过后再扩大机器数量并记录带宽、磁盘和 DHCP 压力。单元测试不代替这些硬件测试。

安装中断后不要直接重用旧授权。先确认目标机器已停止安装并保留日志。确认安装已停止后，可在任务操作中选择“结束失败任务”，输入节点名与原因并确认停止，再创建新的任务。结单只结束记录，不能代替停止客户端安装。
