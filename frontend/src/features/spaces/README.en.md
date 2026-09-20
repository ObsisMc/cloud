# spaces: cloud collaboration space adapter

## Responsibility

Wraps the generated orval client into domain hooks and owns the "current space" context and the SSE subscription. It:

- resolves the tenant via `GET /api/v1/me/tenants` and loads the `GET /spaces` list when a cloud session is active;
- resolves the route's `:workspaceSlug` against real spaces (an unjoined slug resolves to nothing);
- queries and mutates space members and space-scoped projects (create / rename / archive / member upsert), invalidating the affected queries on success;
- subscribes to the `/spaces/:sid/events` SSE stream and only invalidates queries on events (events never carry business state).

It does not: render pages, define routes, sign credentials (devgateway owns that), or serve mock data.

## Files

| File | Purpose |
|---|---|
| `api.ts` | Domain hooks (useSpaces / useSpaceMembers / useSpaceProjects / useCreateSpace / useUpdateSpace / useArchiveSpace / useUpdateSpaceMember) |
| `current-space.tsx` | `CurrentSpaceProvider` + `useCurrentSpace`: cloud/mock dual-mode space resolution |
| `use-space-events.ts` | `parseSSEFrames` (pure) + `useSpaceEvents` (fetch stream subscription and invalidation) |
| `create-space-dialog.tsx` | Create-space dialog: slug normalized to lowercase, reports the slug for navigation |
| `spaces.test.tsx` | Tests for the behaviors above |

## Dependencies and consumers

Depends on: `src/api` (generated client), `src/lib/cloud-session` (credential session), TanStack Query.

May be consumed by: pages and layout components (`projects`, `members`, `settings`, `dashboard-layout`, `app-sidebar`).

## Invariants

- Every cloud query is gated on `tenantId` (`enabled: !!tenantId`); no request fires without credentials and a tenant;
- credentials live only in `cloud-session`; this module reads them and never writes them;
- SSE events only trigger invalidation; authoritative state always comes from REST refetches;
- `parseSSEFrames` is pure: malformed frames are skipped, never thrown.

## Testing

`spaces.test.tsx` mocks the real API with dynamic MSW handlers; without a cloud session the mock fallback path is asserted (`cloudMode=false`, no space). Tests make no network calls.
