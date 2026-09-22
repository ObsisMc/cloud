# internal/controllerpb: Controller gRPC 契约的生成代码

[中文](README.md) | [English](README.en.md)

`internal/controllerpb` 是 Controller 契约 `ora.controller.v1`（消息、`ControllerService` 客户端与
服务端桩）的 Go 生成物。契约的唯一来源是 `ora-space/desktop` 仓库的 `proto/` 目录，由 Controller
作为服务端拥有；本仓库不复制 `.proto`。

## 生成方式

- `proto/buf.gen.yaml` 以 desktop 仓库的 git ref（正式为 `controller-proto/vX.Y.Z` tag）为输入，
  managed 模式指定本包的 `go_package`，插件版本固定。
- `task proto:generate` 重新生成；`task proto:check` 重新生成并在有 diff 时失败，CI 同样执行。
- 本地联调可用 `task proto:generate CONTROLLER_PROTO_INPUT=<desktop/proto 路径>` 覆盖输入，但提交的
  生成物必须来自配置中的 ref。

## 边界与不变量

- **只读**：目录内全部为生成代码，不手工编辑；行为变更只能通过升级 ref 并重新生成。
- **无业务语义**：类型与 Controller 的语义一一对应，租户、用户、成员概念不出现在这里。
- **升级 = 显式 ref 变更**：更新 ref 与重新生成在同一次提交完成，编译失败即契约不对齐。

参见 [internal/controllerclient](../controllerclient/README.md) 与 specs 的
`decisions/cloud/controller-integration/0-cloud-consumes-controller-grpc.md`。
