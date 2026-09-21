# spaces: cloud collaboration space adapter

## Responsibility

Wraps the generated orval client into domain hooks and owns the "current space" context and the SSE subscription. It:

- once the session is signed in, resolves the tenant via `GET /api/v1/me/tenants` (the product never shows tenants, so the first one is taken) and loads the `GET /spaces` list (`useJoinedSpaces`);
- offers `useCreateTenant` (`POST /api/v1/tenants`) for a first-time member without a tenant; the backend creates the first space alongside and makes the caller its owner;
- resolves the route's `:workspaceSlug` against real spaces (an unjoined slug resolves to nothing);
- queries and mutates space members and space-scoped projects (create / rename / archive / member upsert), invalidating the affected queries on success;
- subscribes to the `/spaces/:sid/events` SSE stream and only invalidates queries on events (events never carry business state).

It does not: render pages, define routes, probe the session or log in (`features/auth`), or serve mock data.

## Files

| File | Purpose |
|---|---|
| `api.ts` | Domain hooks (useJoinedSpaces / useCreateTenant / useSpaces / useSpaceMembers / useSpaceProjects / useCreateSpace / useUpdateSpace / useArchiveSpace / useUpdateSpaceMember) |
| `current-space.tsx` | `CurrentSpaceProvider` + `useCurrentSpace`: resolves the route slug to a joined real space and exposes `isPending` / `isError` |
| `slug.ts` | The backend's slug rule (`isValidSlug`) and name-to-slug derivation (`slugFromName`), shared by the create dialog and onboarding |
| `use-space-events.ts` | `parseSSEFrames` (pure) + `reconnectDelay` + `useSpaceEvents` (cookie-session fetch stream, exponential-backoff reconnect and invalidation) |
| `create-space-dialog.tsx` | Create-space dialog: slug normalized to lowercase, reports the slug for navigation |
| `spaces.test.tsx` | Tests for the behaviors above |

## Dependencies and consumers

Depends on: `src/api` (generated client), `src/features/auth/session` (sign-in gate), TanStack Query.

May be consumed by: pages and layout components (`projects`, `members`, `settings`, `dashboard-layout`, `app-sidebar`).

## Invariants

- The tenant list is requested only while the session is `signed-in`; space-level queries and below are gated on `tenantId` (`enabled: !!tenantId`);
- `useJoinedSpaces` answers "no tenant" with `spaces: []`, never `undefined`, so callers distinguish "joined nothing" from "still loading" by `isPending` alone;
- this module never touches credentials; requests ride the gateway cookie the browser sends on its own;
- SSE events only trigger invalidation; authoritative state always comes from REST refetches. A dropped stream reconnects with 1s→30s exponential backoff, a successful reconnect invalidates every query under the tenant, and a 401 ends the subscription;
- `parseSSEFrames` is pure: malformed frames are skipped, never thrown.

## Testing

`spaces.test.tsx` mocks the real API with dynamic MSW handlers; under the signed-out baseline it asserts that no tenant request fires and the context stays pending. Slug rules and the reconnect backoff are pure-function tests. Tests make no network calls.
