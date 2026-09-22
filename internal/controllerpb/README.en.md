# internal/controllerpb: generated Controller gRPC contract

[中文](README.md) | [English](README.en.md)

`internal/controllerpb` is the Go output of the Controller contract `ora.controller.v1` (messages,
`ControllerService` client and server stubs). The single source is the `proto/` directory of the
`ora-space/desktop` repository, owned by the Controller as the server; this repository never copies
the `.proto` files.

## Generation

- `proto/buf.gen.yaml` takes a git ref of the desktop repository as input (a `controller-proto/vX.Y.Z`
  tag once released), sets this package's `go_package` through managed mode, and pins plugin versions.
- `task proto:generate` regenerates; `task proto:check` regenerates and fails on any diff, and CI
  runs the same check.
- For local work, `task proto:generate CONTROLLER_PROTO_INPUT=<path to desktop/proto>` overrides the
  input, but committed output must come from the configured ref.

## Boundaries and invariants

- **Read-only**: everything here is generated; behavior changes only by bumping the ref and regenerating.
- **No business meaning**: types map one-to-one to Controller semantics; tenant, user and membership
  concepts never appear here.
- **Upgrade = explicit ref change**: the ref bump and the regeneration land in one commit; a compile
  failure is the alignment signal.

See [internal/controllerclient](../controllerclient/README.en.md) and the specs decision
`decisions/cloud/controller-integration/0-cloud-consumes-controller-grpc.md`.
