# 本地原型 API v1

已实现单管理员、文章修订、预览、不可变快照；不包含发布任务或生产部署。前端的 `shared/types.ts`、校验器与夹具对应本契约。

## 通用约定

- JSON UTF-8；写入要求 `Content-Type: application/json`，请求体最多 1 MiB，拒绝未知字段及多段 JSON。
- 会话 Cookie `blog_session`：HttpOnly、SameSite=Strict、Path=/api/、12 小时；数据库只保存 token 哈希。生产要求 Secure，本地回环 HTTP 可显式关闭。
- 所有管理接口鉴权；会话写入同时校验准确的 `Origin` 和 `X-CSRF-Token`，登录校验 Origin。token 由登录/me 获取。
- 响应 `Cache-Control: no-store`；错误 `{"error":{"code":"conflict","request_id":"..."}}`，另有 `X-Request-ID`。不返回 SQL、密码或连接信息。
- 400 输入错误、401 身份错误、403 CSRF/来源错误、404 不存在、409 修订/幂等冲突、415 非 JSON、429 登录限流、500 内部错误。
- 登录按直连 peer IP 限制每五分钟十次，最多保留 256 个有效桶；不信任转发头。本地 Nginx 后请求共享桶，适合首轮单管理员原型。

## 健康与身份

| 方法与路径 | 请求与成功响应 |
|---|---|
| GET `/api/health/live` | 200 `{"status":"ok"}` |
| GET `/api/health/ready` | 数据库就绪 200，否则 503 |
| POST `/api/v1/auth/login` | `username`、`password`；200 `username`、`csrf_token`，设置 Cookie |
| GET `/api/v1/auth/me` | 会话；200 `username`、`csrf_token` |
| POST `/api/v1/auth/logout` | 会话、CSRF、JSON `{}`；200 `{"ok":true}`，撤销会话 |

仅允许一个管理员，由 CLI 创建，没有默认密码或注册接口。密码 12–72 字节，bcrypt 成本 10。过期会话在下次成功登录时清理。

## 草稿与修订

保存请求：

```json
{
  "slug": "first-post",
  "title": "第一篇文章",
  "summary": "公开摘要",
  "markdown": "## 正文\n\n你好，博客。",
  "published_at": "2026-09-10T00:00:00+08:00",
  "category": {"slug":"development","name":"开发记录"},
  "tags": [{"slug":"go","name":"Go"}],
  "base_revision_id": 0
}
```

slug 最长 100 字节，小写 ASCII 字母/数字以单个连字符分隔。标题非空、最多 300 UTF-8 字节；摘要 1500 字节，Markdown 200000 字节；分类/标签名称非空、最多 100 字节，最多 20 个不重复标签。日期 RFC3339，1970–9999 年，存为 timestamptz，界面按 Asia/Shanghai 展示。

| 方法与路径 | 行为 |
|---|---|
| GET `/api/v1/admin/posts?offset=0` | 文章 ID 倒序，`{"posts":[],"offset":0,"limit":50}`；offset 0–1000000 |
| POST `/api/v1/admin/posts` | `base_revision_id=0`，创建文章与首个修订，201 |
| GET `/api/v1/admin/posts/:id` | 当前草稿，200 |
| PATCH `/api/v1/admin/posts/:id` | 当前 `revision_id` 作为 `base_revision_id`，保存新修订，200；过期或修改 slug 返回 409 |
| POST `/api/v1/admin/posts/:id/preview` | `{"markdown":"..."}`，文章须存在；返回 `{"html":"安全 HTML"}`，不保存 |

文章响应包含保存字段及 `id`、`revision_id`、`html`。下次保存使用响应的 **revision_id**，不要复用 base_revision_id。slug 创建后不变。预览和保存共用 Goldmark GFM + Bluemonday UGC 净化；不执行原始 HTML、Vue/MDX。旧站特殊 Markdown 语法的完整迁移尚未实现。

## 快照

`POST /api/v1/admin/snapshots`：管理会话、CSRF、额外 `Idempotency-Key`（8–128 字节）。

```json
{"selections":[{"post_id":1,"revision_id":1},{"post_id":2,"revision_id":4}]}
```

- 最多 1000 篇，每篇只选一次，修订须属于指定文章；可以选择历史已保存修订。
- 本次选择是完整内容集，不与旧快照合并。API 允许空数组；当前 UI 要求至少一篇。
- 返回 201 `{"snapshot_id":"...","status":"snapshot_created"}`。规范化选择顺序后计算哈希，同键同选择返回原结果，同键不同选择 409；事务锁保证并发幂等。
- 冻结全部公开字段和站点配置；之后保存草稿不会改变快照。数据库触发器禁止更新/删除修订和快照。同一快照内同一分类/标签 slug 的名称必须一致。
- 没有 deployed 状态、Actions 回调或发布 worker；“快照已生成”不表示上线。

`GET /api/v1/build/snapshots/:id`：独立 `Authorization: Bearer <BUILD_TOKEN>`，管理员 Cookie 不能替代它。

```json
{
  "schema_version": 1,
  "id": "snapshot-id",
  "created_at": "2026-09-10T00:00:00Z",
  "site_title": "顺天的博客",
  "page_size": 5,
  "posts": [{
    "slug": "first-post",
    "title": "第一篇文章",
    "summary": "公开摘要",
    "html": "<h2>正文</h2>\n<p>你好，博客。</p>\n",
    "published_at": "2026-09-10T00:00:00+08:00",
    "category": {"slug":"development","name":"开发记录"},
    "tags": [{"slug":"go","name":"Go"}]
  }]
}
```

文章按日期倒序、slug 升序排列。快照不包含 Markdown、管理员、会话、未选修订和数据库配置。前端拒绝未知版本或额外字段，构建失败而非退回夹具。真实联合验收只下载一次快照，再生成所有页面。
