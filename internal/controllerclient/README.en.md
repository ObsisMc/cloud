# internal/controllerclient: thin Controller gRPC client

[中文](README.md) | [English](README.en.md)

`internal/controllerclient` dials one Controller's `ora.controller.v1.ControllerService`, bounds each
call with a deadline, and turns the Controller's error classification into Go types. It speaks only
the Controller's language and carries no business rules.

## Responsibilities

- **Address parsing**: `Target` accepts exactly the two deployment forms, `tcp://host:port` and
  `unix:///absolute/path`, converting them to grpc-go targets; anything else is refused before dialing.
- **Connection**: `Dial` opens a plaintext connection (h2c over TCP, h2 over a Unix socket), matching
  the Controller's unauthenticated loopback and socket transports; `New` adopts an existing connection
  for tests.
- **Calls**: `AcceptClone`, `ListOperations` and `GetOperation` map one-to-one to the contract, each
  bounded by `timeout`.
- **Error classification**: failures with a gRPC status become `Error{Code, Status, Message}`. `Code`
  comes from the Controller's attached `ErrorDetail`; without a detail it is `UNSPECIFIED` while
  `Status` stays usable. Deadline and cancellation pass through unwrapped.

## Boundaries and invariants

- **No business logic, caching or implicit retries**: retry semantics (same `request_id` returns the
  original receipt) are the caller's business decision.
- **Tenancy happens first**: Cloud authorizes the tenant before calling the Controller; neither this
  package nor the contract has tenant fields.
- **Transport failures stay distinguishable**: `UNAVAILABLE` without a detail is a connection problem;
  with a detail it is the Controller reporting unavailable persistence.

Tests use an in-process stub (`bufconn`) for address validation, error mapping and deadlines; the
contract test against a real `ora-controller` is registered once the Controller serves gRPC, gated by
`ORA_CONTROLLER_BIN`.
