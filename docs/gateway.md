# 认证 Gateway

Gateway（`cmd/gateway`）是浏览器可访问的公开认证与反向代理边界，实现 `specs/decisions/cloud/identity-access/0-gateway-mediated-external-identity.md`。它完成外部登录、在 PostgreSQL 中保存浏览器会话、为每个已认证请求签发短期内部 service/user JWT，并只把 `/api/v1/*` 转发到固定配置的 Cloud upstream。Cloud 继续拥有用户、identity 映射、JIT 创建、membership 和资源授权；Gateway 不读写任何 Cloud 业务表，也不在启动时执行 DDL。

## 路由

| 路由 | 说明 |
|---|---|
| `GET /auth/providers` | 返回 `{"providers":[...]}`，即本部署注册的 provider 名称（排序后），前端据此决定显示哪些登录按钮。无输入、不泄露配置细节。 |
| `POST /auth/login` | 同源 JSON：`{"provider":"github","returnTo":"/path"}`。校验 `Origin`（或 `Sec-Fetch-Site: same-origin`）、限流后写入 Login Attempt，返回 `{"authorizationUrl"}` 并设置 attempt Cookie。`returnTo` 只接受以单个 `/` 开头且第二个字符不是 `/` 或 `\` 的相对路径；不存在 `GET` 形式。 |
| `GET /auth/callback/{provider}` | provider 回跳。同时匹配 attempt Cookie、`state`、provider、未过期、未消费；在事务外向 provider 交换 code 与 PKCE verifier；再在一个短事务里锁定 attempt、写入 `consumed_at` 并创建 session。成功 `303` 到 attempt 中保存的 `returnTo`，失败统一 `401 login_failed`。无论成败都清除 attempt Cookie。 |
| `POST /auth/logout` | 同源校验后吊销当前 session（`revoked_reason=logout`）并清除 Cookie；幂等，返回 `204`。 |
| `ANY /api/v1/*` | 解析 session Cookie，失败返回 `401 unauthenticated`。修改状态的方法要求同源证明（`403 origin_forbidden`）。丢弃浏览器提供的 `Authorization`、`X-Ora-User-Token`、`Cookie`、`Forwarded`/`X-Forwarded-*`，用本副本的两把私钥签发 service/user JWT 后转发。Cloud 不可达或在 `cloud.timeout` 内未完成响应返回 `502 upstream_unavailable`，session 不受影响。Cloud 以 `text/event-stream` 应答时（空间事件流），该连接不再受 `cloud.timeout`、`server.write_timeout` 与 8 MB 响应体上限约束，逐帧转发直到任一方关闭；限制只对已授权的这一个连接放开。 |
| `GET /healthz` | PostgreSQL 探活。 |
| `GET`/`POST /auth/dev/authorize` | 仅当 `login.development_provider` 开启时存在：本地开发登录表单，见下文。 |

`/internal/v1/*` 没有路由，落到 `404 not_found`。所有错误使用 `{"code","params","requestId"}`，与 Cloud 一致。

## Cookie

- Session Cookie：生产为 `__Host-ora_session`（`HttpOnly`、`Secure`、`SameSite=Lax`、`Path=/`、无 `Domain`），`Max-Age` 等于数据库 `expires_at` 与当前时间之差，不会晚于数据库期限。开发 loopback HTTP 下为 `ora_session` 且无 `Secure`。
- Attempt Cookie：`__Secure-ora_login`（开发为 `ora_login`），`SameSite=Lax`（provider 回跳是跨站顶层导航，`Strict` 会让 callback 永远收不到），`Path=/auth/callback`，`Max-Age` 等于 attempt 有效期。

## 持久化

`internal/core/migrations/0005_gateway_auth.sql` 创建 `gateway_login_attempts` 与 `gateway_sessions`。数据库只保存 attempt secret、`state` 与 session token 的 SHA-256 digest（`bytea`，长度 32），约束保证 `expires_at > created_at`、session 不超过创建后 90 天、attempt 不超过 1 小时、`revoked_at` 与 `revoked_reason` 同时存在且 reason 只能是 `logout`/`identity_revoked`/`administrative`，`return_to` 在数据库层也拒绝绝对、`//` 和 `/\` 形式。

PKCE verifier 不落库：由 attempt secret 和 `login.pkce_key_file` 通过带域分离的 HMAC-SHA256 派生。所有副本必须配置同一把 PKCE 密钥，否则回跳到另一副本时 provider 会拒绝 verifier（集成测试覆盖了这一点）。

有效性只由数据库状态和 `clock_timestamp()` 决定；读取不续期。`Revoke`/`RevokeIdentity` 只作用于尚未过期的 session，过期行保留其过期事实作为审计。清理由 Gateway 生命周期拥有的 goroutine 按 `session.cleanup_interval` 运行，每批最多 `session.cleanup_batch` 行，只删除超过 `session.retention` 的过期/吊销 session 与过期 attempt。

## 配置与密钥

参见 `configs/gateway.yaml`。环境变量前缀为 `GATEWAY_`。启动时校验：

- `public.base_url` 必须是 `https` origin（不含路径/查询）；只有 `public.development: true` 且 host 为 `localhost`/`127.0.0.1`/`::1` 时允许 `http`。callback URL 固定为 `<base_url>/auth/callback/<provider>`，不从请求 header 推导。
- `session.ttl` 为正且不超过 90 天，缺省 30 天；`login.attempt_ttl` 在 1 分钟到 1 小时之间；`tokens.lifetime` 不超过 5 分钟（Cloud 验证器上限）。
- `tokens.service_private_key_file` 与 `tokens.user_private_key_file` 是两把不同的 PKCS#8 Ed25519 私钥，分别对应 Cloud `auth.keys` 中 `kind: service, role: gateway` 与 `kind: user` 的公钥条目；一把私钥不能同时承担两个用途。
- `login.pkce_key_file` 至少 32 字节随机数据；`github.client_secret_file` 是 OAuth App client secret。这些文件只读入进程内存，从不写日志或数据库。
- 至少要有一个 provider：`github.client_id` 非空则注册 GitHub（此时必须给出 `client_secret_file`），`login.development_provider: true` 则注册 `dev`。后者只在 `public.development: true` 时通过校验，生产配置不得设置。

生成密钥示例：

```bash
openssl genpkey -algorithm ed25519 -out gateway-service.key && openssl pkey -in gateway-service.key -pubout -out gateway-service.pem
```

```bash
openssl genpkey -algorithm ed25519 -out user-identity.key && openssl pkey -in user-identity.key -pubout -out user-identity.pem
```

```bash
head -c 32 /dev/urandom > gateway-pkce.key
```

`.key` 交给 Gateway，`.pem`（PKIX PUBLIC KEY）交给 Cloud 的 `auth.keys`。

## 本地开发 provider（`dev`）

`internal/gateway/devlogin` 让前端在没有 GitHub OAuth App 时也能登录：`POST /auth/login` 传 `provider: "dev"`，`authorizationUrl` 指向 Gateway 自己的 `GET /auth/dev/authorize`（携带 `state` 与 `code_challenge`），开发者在表单里输入 `source` / `subject` / `display_name`（默认 `dev` / `developer` / `Developer`），同源 `POST` 后表单用进程内随机密钥的 HMAC 封装身份、challenge 与 5 分钟有效期得到 code，`303` 回 `/auth/callback/dev`。之后的 attempt 校验、PKCE verifier 校验、session 创建、Cookie 与凭据签发与 GitHub 完全相同；Cloud 仍按 `(source, subject)` 创建或解析用户，membership 与授权不因 provider 而异。输入任意 source（例如 `cloudctl bootstrap` 用的 `huawei-corp`）即可以该身份登录。该 provider 只能与 `public.development` 同时开启，启动日志会给出 warning。

## GitHub 适配器

`internal/gateway/github` 使用 OAuth App Authorization Code Flow，不请求任何 scope，authorize 请求携带 `state`、`code_challenge`（S256）与固定 `redirect_uri`；GitHub 在收到 challenge 后要求 token exchange 携带 `code_verifier`。适配器读取 `GET /user` 后立即丢弃 access token，只返回 `VerifiedIdentity{source: "github.com", subject: 数字 id 的十进制字符串, displayName: name 或 login}`。`displayName` 在离开编排层前按字节截断到 200，与 Cloud `identity()` 的上限一致。配置 `authorize_url`/`token_url`/`user_url`/`source` 可指向 GitHub Enterprise Server，此时 `source` 是该 host。

## 运行时数据库角色

生产部署应为 Gateway 配置只拥有 `gateway_login_attempts`、`gateway_sessions` 上 `SELECT/INSERT/UPDATE/DELETE` 以及 `schema_migrations` 上 `SELECT` 的角色。Gateway 启动只调用 `CheckSchema` 校验 migration 校验和，绝不执行 DDL；migration 仍由 `cloudctl migrate` 应用。

## 本地运行

```bash
cp config.toml.template config.toml && task setup && task dev
```

`config.toml`（被 Git 忽略）集中填写 PostgreSQL DSN；GitHub OAuth App 的 client id / client secret 可选，留空则只有本地开发登录（`configs/gateway.yaml` 已开启 `login.development_provider`）。`task setup`（`cmd/devsetup`）据此在 `.local/gateway/` 生成两把 Ed25519 私钥、对应公钥与 PKCE 密钥（重复执行不会覆盖已有密钥），把 client secret（若有）写入 `.local/gateway/github-client-secret`，写出 `.local/dev.env`（`GATEWAY_GITHUB_CLIENT_ID`、`GATEWAY_DATABASE_DSN`、`CLOUD_DATABASE_DSN`、`TEST_DATABASE_URL`，`Taskfile.yml` 为每个 task 自动加载）并应用迁移；`configs/config.yaml` 的 `auth.keys` 与 `configs/gateway.yaml` 已指向这些文件，仍是权威配置。`task dev` 同时启动 Cloud（:8080）、Gateway（:8081）与 Vite 前端（:5173）。浏览器只访问 `http://localhost:5173`：Vite 把 `/auth`、`/api`、`/healthz` 代理到 Gateway，因此 `public.base_url` 是 `http://localhost:5173`，Cookie、同源校验与 OAuth callback（`http://localhost:5173/auth/callback/github`，需在 GitHub OAuth App 中登记）都以它为准。登录页会向 `GET /auth/providers` 询问可用的登录方式，本地默认显示"开发者登录"，配置了 GitHub 后再多一个 GitHub 按钮。前端页面见 `frontend/src/features/auth`。
