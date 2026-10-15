# 同机准备 iVentoy 安装文件

管理端为已授权的 Linux 任务生成脚本和独立 ISO 硬链接，不复制 ISO 内容、不改写 iVentoy 私有配置。镜像、脚本和分机独启规则仍在 iVentoy 原生界面关联。

## 配置

管理端配置增加 `iventoy_root`，例如 `/opt/iventoy`，并把 `iso_dir` 设为 `/opt/iventoy/iso`。使用不含版本号、没有空格的真实目录，方便后续升级；镜像须为普通 ISO 文件，本功能不接受符号链接镜像。文件系统需支持硬链接（XFS/ext4/NTFS；exFAT/FAT 不支持）。管理端验证标准目录和 `data/iventoy.dat` 是否存在。

Linux 下创建两个专用目录（替换示例路径）：

```sh
sudo install -d -o ventoy-center -g ventoy-center -m 0700 \
  /opt/iventoy/iso/ventoy-center \
  /opt/iventoy/user/scripts/ventoy-center
```

源 ISO 应属于 `ventoy-center` 且可读，以符合 Linux 硬链接保护规则。导入新镜像可用 `install -o ventoy-center -g ventoy-center -m 0440 源ISO 目标ISO`，不要覆盖正在使用的镜像。父目录须允许该用户遍历；iVentoy 以 root 运行，可读取受限脚本。不要关闭 `fs.protected_hardlinks`。

在 `/etc/systemd/system/ventoy-center.service.d/iventoy.conf` 增加：

```ini
[Service]
ReadWritePaths=/opt/iventoy/iso /opt/iventoy/user/scripts/ventoy-center
```

把整个 `iso` 置于同一可写挂载内，避免源文件和硬链接跨挂载失败；通过 Unix 权限限制服务只能在专用目录新建文件。iVentoy 位于 `/home` 时，另设 `ProtectHome=read-only` 并替换路径。执行 `systemctl daemon-reload` 后重启管理端。

Windows 使用运行管理端的专用账户，为两个专用目录授予修改权限，为源 ISO 授予读取权限。脚本包含任务凭据，NTFS ACL 应限制为管理账户和 SYSTEM；管理端不自动改写 ACL。

## 操作

1. 创建任务，核对机器与磁盘后授权。
2. 打开任务的“iVentoy 文件”，点击“准备安装文件”。创建 `iso/ventoy-center/任务号/image.iso` 和 `user/scripts/ventoy-center/任务号/` 内的脚本、清单。
3. 按弹窗中的路径关联 iVentoy 镜像与脚本，设置默认脚本及非零超时，再在专业版分机独启绑定准确 MAC。
   CentOS 7.9 和 Rocky 8 还需为同一镜像设置弹窗显示的“启动注入文件”：分别为 `centos7-iventoy.tar.gz` 和 `rocky-iventoy.tar.gz`，用于补上 iVentoy 1.0.44 的 dracut Kickstart 入口；它不能代替自动安装脚本。Rocky 9 / 10 不使用这两个注入包。
4. 客户端从网卡启动。文件已准备不代表 PXE 已启动或安装成功。
5. 任务结束后先移除 iVentoy 的关联，再点击“清理任务文件”。只清理清单中的文件与硬链接，原始 ISO 保留。

专用 `ventoy-center` 子目录不会作为新任务的原始镜像来源。Windows 仍使用 ZIP 安装包。

升级前已准备的任务不会原地改写。若旧任务缺少新版本要求的注入包，应在客户端停止使用镜像后取消任务、移除 iVentoy 绑定、清理任务文件，再创建并绑定新任务。

## 中断与冲突

准备操作先持久保存清理清单，再创建文件。失败或重启后可重试有效的待启动任务；相同文件会核对复用。取消任务后可清理部分结果。发现内容变化、未知文件、符号链接或源镜像替换时保留现场，由管理员核对后处理。断电遗留的不完整文件也需人工核对。不要修改正在使用的源 ISO，硬链接共享内容。

接口均需管理口令：`GET /api/iventoy`、`GET /api/jobs/{id}/preparation`、`POST /api/jobs/{id}/prepare`、`POST /api/jobs/{id}/cleanup`。`manual_binding` 始终为 `true`。
