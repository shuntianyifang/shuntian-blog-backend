# Shuntian Blog Backend

新博客 Go API 服务。第一轮本地原型已实现并通过真实 PostgreSQL 与浏览器验收：单管理员、文章修订、预览、不可变快照。尚无生产部署或自动发布工作流。

## 职责

已实现认证与会话、文章修订及公开内容快照；媒体、GitHub Actions 和上线状态留待后续。本项目独立私有仓库为 [shuntian-blog-backend](https://github.com/shuntianyifang/shuntian-blog-backend)，默认分支 main；前端仓库为 [shuntian-blog-frontend](https://github.com/shuntianyifang/shuntian-blog-frontend)。文章归档仓库 shuntian-blog-content 仍为规划，不在本次建仓范围。

PostgreSQL 是唯一权威来源。草稿保存、请求发布、网站上线是不同状态。文章归档为单向导出，不做双向自动同步。

## 本地环境与启动

版本：Go 1.25.0，PostgreSQL 18（本机 18.6）。依赖锁在 go.mod/go.sum；脚本兼容 Windows PowerShell 5.1 与 PowerShell 7，使用 PATH 中的 Go。以下命令从本仓库根目录执行，不依赖相邻目录。

```powershell
go mod download
./scripts/setup-db.ps1
./scripts/run.ps1 dev migrate
./scripts/run.ps1 dev admin your-name
./scripts/run.ps1 dev serve
```

初始化优先使用已有 libpq 凭据，否则在本机安全提示中输入 PostgreSQL 管理员密码，不在聊天中传递。默认 psql 路径 D:/PostgreSQL/18/bin/psql.exe，可用 `-Psql` 覆盖。脚本只管理本机 5432 的 shuntian_blog_dev/test 与各自 owner/app 角色；未知同名库或不完整配置会停止，不重置账号或数据。

本地 `.local/dev.env`、`.local/test.env` 保存随机生成的应用/迁移连接串和构建 token，被 Git 忽略。`.env.example` 说明字段。迁移使用 owner，API 使用 app；运行账号不能建表、修改快照或跨开发/测试库连接。迁移只追加，校验已应用文件哈希。

交互管理员命令使用本机隐藏密码输入。自动化可临时设置 ADMIN_PASSWORD，不能提交；`scripts/demo-admin.ps1` 可生成随机的 local-admin，凭据只写 `.local/demo-admin.json`，已有管理员不替换。本工作区验收已创建此演示账号，不要再次运行创建管理员命令。

可选后台运行：`./scripts/start-local.ps1` 构建并启动本机 8080 API，日志和 PID 位于 `.local/`；`./scripts/stop-local.ps1` 校验进程路径后停止该实例。前台 `serve` 使用 Ctrl+C 优雅退出。

默认 PUBLIC_ORIGIN 为 http://127.0.0.1:8081，配合独立 Nginx。使用 Nuxt 开发端口 3000 时，在本地配置中改为对应 origin 再启动 API。COOKIE_SECURE=false 仅允许回环 HTTP；生产配置必须使用 HTTPS 和安全 Cookie。

## 接口与测试

契约见 [docs/API.md](docs/API.md)。

```powershell
gofmt -l cmd internal
go vet ./...
go test ./...
./scripts/test.ps1
```

普通 go test 在未配置 TEST_DATABASE_URL 时明确跳过数据库测试；完整验收必须运行 test.ps1。它强制本机 shuntian_blog_test 和专用角色，验证重复迁移、权限隔离、认证/CSRF、修订冲突、并发幂等、快照不可变与内容隔离；自动保留随机测试管理员凭据，不清空开发库。重复执行会追加少量测试文章/快照，不删除已有记录。

Linux/macOS 可使用相同 Go 命令，安全导出 `.env.example` 对应变量；PowerShell 脚本仅负责本地环境准备，不是生产数据库自动化方案。

## 发布与部署

未来内容发布触发前端 Actions，后端代码部署使用独立工作流。当前只支持本地固定快照构建；Go 监听本机，由本地 Nginx 转发。没有操作 Caddy、VPS 或代理。

systemd、备份恢复、异地备份、生产迁移及发布回滚尚未实现或验收。未来部署必须兼容仍在线的旧静态前端；静态回滚不等于数据库回滚。

## 开发规则

参见 [AGENTS.md](AGENTS.md)。

## 版权与复用

本项目作者的原创代码、文章、图片和文档暂未授予开源或内容复用许可。未经作者明确许可，不授权他人复制、修改、转载、再分发或商业使用；依法允许的使用除外。第三方代码、引用和素材遵循各自原有许可，本说明不撤销或覆盖第三方已授予的权利。暂不新增 LICENSE，也不将项目标记为 MIT、Apache 或 Creative Commons 等许可。
