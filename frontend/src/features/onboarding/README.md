# onboarding：首个工作区

[中文](README.md) | [English](README.en.md)

## 职责

已登录但还没有任何工作区的成员在这里创建第一个。它是 `/onboarding` 路由，也是 `/` 的落点：已有工作区的成员会被直接送去该工作区，因此这个页面只服务首次使用。

表单只有名称与工作区地址：地址输入框前固定显示保留前缀 `host/w/`（不可编辑），只有其后的 slug 可改（从名称自动派生，用户改过后不再跟随），下方展示完整地址。提交时按成员状态选择接口：没有租户则 `POST /api/v1/tenants`（后端隐式建租户并把调用者设为管理员与空间 owner，产品从不展示租户）；已有租户但没有存活空间则 `POST /tenants/{tid}/spaces`。两者成功后都进入 `/w/{slug}/issues`。

不负责：登录（`features/auth`）、之后再建工作区（侧栏的 `CreateSpaceDialog`）、租户的任何可见管理。

## 文件

| 文件 | 说明 |
| --- | --- |
| `onboarding-page.tsx` | `OnboardingPage`：加载/错误/已有工作区跳转，以及 `CreateFirstWorkspace` 表单 |
| `onboarding-page.test.tsx` | 跳转、slug 派生、两条创建路径、错误码展示、非法 slug 禁用、退出登录 |

## 依赖方向

依赖：`features/auth/session`（显示名与退出）、`features/spaces/api`（`useJoinedSpaces` / `useCreateTenant` / `useCreateSpace`）、`features/spaces/slug`、`src/lib/paths`、UI 组件。

可被依赖：`routes.tsx`。

## 不变量

- 页面必须在 `RequireSession` 之内渲染；它假定会话已确认。
- 有工作区的成员绝不会看到表单，而是被 `Navigate` 送走；这使 `/` 可以无条件重定向到这里。
- 工作区永远位于 `src/lib/paths` 的 `WORKSPACE_ROUTE_PREFIX` 之下，slug 不可能与 `/login`、`/onboarding` 等应用路由冲突。
- 提交前 slug 必须通过 `isValidSlug`，与后端规则相同；后端的拒绝以 fault code 原样展示并保留表单。

## 测试

`onboarding-page.test.tsx` 通过 `renderRoutes` 挂真实路由，用 MSW 区分"无租户"、"有租户无空间"与"已有空间"三种前置状态。
