# 开发与验证

```sh
go test ./...
go vet ./...
go test -race ./...  # 支持 race 工具链的 Linux 环境
node --check internal/center/web/app.js
```

测试覆盖管理接口认证和来源检查、敏感字段不出现在任务列表、并发单次领取、重启后的重放拒绝、取消/过期、状态顺序、连接文件领取阶段、路径边界、模板渲染与 Windows XML 格式。

测试文件中的 ISO / RPM / DEB 都是字符串夹具，不可用于装机。可设置 `VENTOY_TEST_OUTPUT` 导出测试模板，仅用于语法检查，不把它们复制到生产 iVentoy。

CI 在 Linux 和 Windows 测试 Go 代码，在 Linux 运行 race 检查，输出两种平台的二进制包。另外用 Rocky 8/9/10、Debian 12/13、Ubuntu 24.04/26.04 容器检查静态二进制启动。CI 的容器步骤不能证明 PXE、硬件驱动或完整操作系统安装成功。

使用固定提交的官方 GitHub Actions。不要把 ISO、凭据、私有 fyhocsys 源码或运行状态提交到公开仓库。
