# Settings

设置模块包含成员、模型连接、主题、Plugins 和 Skills 五个独立子路由，默认进入成员页。页面沿用 V2 的组件和语义颜色，不引入其他 UI 库。

## 数据边界

成员页是真实的：列表来自 `GET /api/v1/tenants/{tid}/members`，owner 还能填邮箱邀请（`POST /api/v2/tenants/{tid}/members`，契约之外的 v2-server 路由），可选同时加入一个小队。v2 没有改角色、停用或移除成员的接口，页面因此不提供这些操作；邀请入口按 `GET /api/v1/me/tenants` 里的真实角色只对 owner 显示，服务端同样会拒绝 member。

除真实登录状态、成员与浏览器主题偏好之外，模型连接、Plugins 与 Skills 三个页面仍是明确标注的演示，按登录用户隔离。不会调用模型、插件或技能 HTTP 接口，不会安装软件或执行模型。演示写入的权限也跟随真实角色：只有 owner 能在演示里安装/移除插件。不存在请求失败后自动切换假数据的逻辑。

`data/repository.ts` 是本机存储边界，键包含版本、当前用户 ID 和演示空间 ID。`members/` 不使用它。初次读取创建示例；修改先校验 revision 并成功写入 localStorage，再更新 TanStack Query 缓存。保存失败和损坏数据会报错，不会自动清空。用户明确确认后才能重置。此数据不跨设备同步，不是正式服务端存储或授权实现。

- 插件复用生成的 `PluginCatalogEntry` 与 `SpacePlugin`；以 `sourceNamespace/identifier` 对应，目录行 ID 与安装行 ID 不混用。安装意图与结果分开保存；模拟安装直接完成本地状态变化，不伪造下载进度。只演示非 Agent 插件；合集只读。
- 技能是独立的说明文档，保留 namespace、source、availability、content。自建技能可修改；插件技能只读，安装/移除改变可用性，移除后仍能预览文档。预览不执行 HTML 或加载远程图片。
- 成员列表复用契约生成的 `MemberListItem`，角色显示为 `owner` / 成员；invite 客户端由 `orval.members.config.ts` 从 `v2-server/members.openapi.json` 生成。邀请填邮箱（必填）、显示名与小队（选填）；小队候选来自「协作」的 `GET /api/v2/tenants/{tid}/squads`，只列未归档小队，没有可加入的小队时不显示这一项。演示数据里不再保存成员、申请或邀请链接。
- 模型连接复用生成类型，维护元数据、多模型和默认选择；只模拟 credentialConfigured，不提供真实 API Key 输入。默认连接失效时提示重新选择，不悄悄替换模型。

## 结构

- `page.tsx`、`navigation.ts`、`index.ts`：设置容器、导航、懒加载子路由。
- `members/`、`model-connections/`、`theme/`、`plugins/`、`skills/`：各页界面、表单与测试。
- `components/`：设置页面共用的状态、确认、详情和 Markdown 展示。
- `api/generated.ts`：邀请接口的生成客户端，`npm run api:generate:members` 生成，`npm run check` 校验漂移。
- `data/`：独立类型、示例、持久化边界与领域操作；只通过这里更新演示状态。

主题由应用级 next-themes 管理，默认浅色，浏览器键为 `ora-v2-theme`。只有用户明确选择“跟随系统”后才读取系统外观；已有的浅色、深色或跟随系统偏好继续生效。Vite 的 createRoot 模式不会执行组件里的 SSR 脚本，因此 `main.tsx` 在挂载前应用初始主题，provider 的 SSR 脚本显式保持 inert；之后由 provider 管理主题及系统变化。现有定时页仅补深色变量适配。

## 验证

`npm run check` 检查类型、Oxlint、单元测试、客户端一致性与构建。`e2e/login.ts` 提供邮箱登录辅助，`e2e/settings.spec.ts` 覆盖五页路由、成员页的 owner/member 两种视角、浏览器持久化、错误恢复、主题、375/768/1440 布局与截图；`e2e/multiuser.spec.ts` 覆盖 owner 邀请（含邀请入队）、被邀请者登录、未邀请邮箱被拒与 member 被服务端拒绝。模型、插件与技能的资源操作验收的是本机演示，不代表后端能力已经实现。
