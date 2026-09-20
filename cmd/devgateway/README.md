# devgateway：本地开发凭证桥

[中文](README.md) | [English](README.en.md)

## 职责

仅限开发环境的前端凭证桥，模拟生产拓扑「浏览器 → 网关 → Cloud」：

- `GET /devgateway/login?source=&subject=&display=`：用本地 Ed25519 私钥签发短时双 JWT（service=gateway + user，`caller` 绑定），返回 `{serviceToken, userToken, expiresAt}`（5 分钟有效）；
- 其余路径无状态透传到 Cloud（默认 `http://127.0.0.1:8080`），浏览器请求头中的双凭证原样转发。

私钥永不进入前端代码。密钥默认复用 `.local/minttoken/keys/private.pem`（缺失时自动生成并写出 `public.pem`，提示配置 `configs/config.yaml` 的 `auth.keys`）。**禁止部署到任何非本地环境。**

## 使用

```powershell
go run ./cmd/devgateway -addr 127.0.0.1:8090 -cloud http://127.0.0.1:8080
```

前端 Vite dev server 将 `/api`、`/internal`、`/healthz`、`/devgateway` 代理到本服务。生产环境由真实网关承担签发与转发，本命令不参与。
