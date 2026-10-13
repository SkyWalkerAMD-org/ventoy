# 开发与验证

```sh
go test ./...
go vet ./...
go test -race ./...  # 支持 race 工具链的 Linux 环境
node --check internal/center/web/app.js
```

测试覆盖管理接口认证和来源检查、敏感字段不出现在任务列表、并发单次领取、重启后的重放拒绝、取消/过期、状态顺序、连接文件领取阶段、路径边界、模板渲染与 Windows XML 格式。

版本测试遍历全部 13 个 profile 的 UEFI / BIOS 配置（Windows 11 BIOS 应拒绝），核对单次安装状态流转和安装包校验和。Linux CI 会执行 Debian guard 的隔离测试：用临时假 sysfs / 设备和命令替身验证 MAC 不符、序列号不符/重复、USB、RAID、固件不符、依赖缺失及授权拒绝均不会写入 partman 分区确认。

此测试只执行授权钩子，不执行安装器、真实块设备、磁盘分区或外部网络操作。保留旧接入任务测试用于兼容性；新建 `enroll: true` 必须拒绝。

Linux CI 另用 Pykickstart 3.78 的 RHEL7/8/9/10 解析器检查 Kickstart，使用 PyYAML 6.0.3 解析 Ubuntu 配置，并对 22 份 Linux 模板及内嵌脚本运行 `bash -n`。这能发现语法错误，不等于安装器运行验证或所有 Subiquity 版本的 schema 验证。本地可在安装上述两个 Python 依赖后运行 `python scripts/validate_templates.py 导出目录`。

测试文件中的 ISO / RPM / DEB 都是字符串夹具，不可用于装机。可设置 `VENTOY_TEST_OUTPUT` 导出测试模板，仅用于语法检查，不把它们复制到生产 iVentoy。

CI 在 Linux 和 Windows 测试 Go 代码，在 Linux 运行 race 检查，输出两种平台的二进制包。另外用 Rocky 8/9/10、Debian 12/13、Ubuntu 24.04/26.04 容器检查静态二进制启动。CI 的容器步骤不能证明 PXE、硬件驱动或完整操作系统安装成功。

使用固定提交的官方 GitHub Actions。不要把 ISO、凭据、私有 fyhocsys 源码或运行状态提交到公开仓库。
