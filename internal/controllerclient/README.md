# internal/controllerclient: Controller gRPC 薄客户端

[中文](README.md) | [English](README.en.md)

`internal/controllerclient` 拨号一个 Controller 的 `ora.controller.v1.ControllerService`，为每次调用
设置截止时间，并把 Controller 的错误分类解成 Go 类型。它只讲 Controller 的语言，不含任何业务规则。

## 职责

- **地址转换**：`Target` 只接受部署使用的两种形式——`tcp://host:port` 与 `unix:///absolute/path`——并
  转换为 grpc-go target；其他形式在拨号前拒绝。
- **连接**：`Dial` 建立明文连接（TCP 上为 h2c，Unix socket 上为 h2），与 Controller 当前无认证的回环
  与 socket 传输一致；`New` 接受已有连接供测试使用。
- **调用**：`AcceptClone`、`ListOperations`、`GetOperation` 与契约一一对应，每次调用受 `timeout` 约束。
- **错误分类**：带 gRPC 状态的失败解成 `Error{Code, Status, Message}`。`Code` 来自 Controller 附带的
  `ErrorDetail`，没有 detail 时为 `UNSPECIFIED` 且 `Status` 仍可用；截止时间与取消原样透传，不包装。

## 边界与不变量

- **无业务、无缓存、无隐式重试**：重传语义（同 `request_id` 返回原回执）由调用方按业务决定。
- **租户在调用之前**：Cloud 完成租户授权后才调用 Controller；本包与契约都没有租户字段。
- **传输失败可区分**：无 detail 的 `UNAVAILABLE` 是连接问题，带 detail 的 `UNAVAILABLE` 是 Controller
  自报的持久化不可用。

测试以进程内 stub（`bufconn`）验证地址校验、错误映射与截止时间；对真实 `ora-controller` 的契约测试
在 Controller 侧 gRPC 服务落地后按 `ORA_CONTROLLER_BIN` 门控登记。
