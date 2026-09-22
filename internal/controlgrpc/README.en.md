# internal/controlgrpc: gRPC server of the Controller internal control contract

[中文](README.md) | [English](README.en.md)

`internal/controlgrpc` serves the [`internal/controlpb`](../controlpb/README.en.md) contract over gRPC. It
is a translation layer only: every RPC verifies the caller's service credential, converts the request
into a `core.ControlRequest`, runs it through the same `Store.Control` transaction as the JSON internal
API, and maps the resulting `Fault` to a gRPC status. No business rule, cache or implicit retry lives here.

## Authentication

- `authorization: Bearer <service JWT>` metadata, verified by `core.Authenticator` as `kind=service`;
  only `role=controller` is admitted (missing → `UNAUTHENTICATED`, other roles → `PERMISSION_DENIED`).
- The lease holder is the verified service `sub`, never a request field.
- Unary and stream interceptors share one verification; later server streams inherit it.

## Error mapping

The status code is the primary classification and `ErrorDetail{ErrorCode}` is attached as a
`google.rpc.Status` detail; both are decided in one place (`fault.go`):

| `Fault` | gRPC status | `ErrorCode` |
|---|---|---|
| `lease_held` | `FAILED_PRECONDITION` | `LEASE_HELD` |
| `stale_controller`, `stale_operation` | `FAILED_PRECONDITION` | `STALE_CONTROLLER` |
| other 409 | `ABORTED` | `CONFLICT` |
| 400 | `INVALID_ARGUMENT` | `INVALID_INPUT` |
| 403 | `PERMISSION_DENIED` | `SERVICE_FORBIDDEN` |
| 404 | `NOT_FOUND` | `NOT_FOUND` |
| database failure | `UNAVAILABLE` (no database detail) | `UNAVAILABLE` |

## Implemented services

- `ControllerLeaseService`: `AcquireLease` / `RenewLease` / `ReleaseLease` map to `lease_acquire` /
  `lease_renew` / `lease_release`; one global lease, 30-second expiry, monotonic `epoch`, expiry judged
  by the database clock.

`ExecutionService` and `ControlSignalService` are registered by later changes. The listen address comes
from `control.grpc_addr` and must stay on a loopback or private network until TLS lands.
