# cmd/devkeys：本地开发密钥生成器

[中文](README.md) | [English](README.en.md)

`cmd/devkeys` 只服务于本地开发：让 `task dev` 能在一台笔记本上启动真实的 Gateway 与 Cloud，而不需要手工执行 `openssl`。生产密钥由部署基础设施提供，本命令不参与。

## 职责

- 在 `.local/gateway/`（`-dir` 可改）生成两对用途分离的 Ed25519 密钥：`gateway-service.key`/`.pem` 与 `user-identity.key`/`.pem`。私钥为 PKCS#8 PEM 交给 Gateway（`configs/gateway.yaml`），公钥为 PKIX PEM 交给 Cloud（`configs/config.yaml` 的 `auth.keys`）。
- 生成 32 字节随机 `gateway-pkce.key`。
- 提示缺失的 `github-client-secret` 文件；该文件由开发者自行写入 GitHub OAuth App 的 client secret，client id 通过 `GATEWAY_GITHUB_CLIENT_ID` 提供。

## 边界与不变量

- **幂等且不覆盖**：已存在的文件一律保留；一对密钥的私钥与公钥必须同时存在，否则报错要求删除后重生成，避免 Gateway 与 Cloud 信任的密钥漂移。
- **不输出私钥**：只打印“已生成/已保留”的文件名。
- **只写 `.local/`**：该目录被 Git 忽略；任何生成物都不进入仓库、日志或数据库。
- **禁止部署**：文件权限为 `0600`，但没有任何轮换、托管或分发机制。

参见 [cmd 入口总览](../README.md) 与 [Gateway 文档](../../docs/gateway.md)。
