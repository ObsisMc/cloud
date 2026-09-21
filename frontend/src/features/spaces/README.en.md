# spaces: collaboration space adapter

## Responsibility

Wraps the generated orval client into domain hooks and owns the space SSE subscription. It:

- resolves the tenant via `GET /api/v1/me/tenants` and loads the `GET /spaces` list;
- queries and mutates space members and space-scoped projects (create / rename / archive / member upsert), invalidating the affected queries on success;
- subscribes to the `/spaces/:spaceId/events` SSE stream and only invalidates queries on events (events never carry business state).

Authentication rides the ora-web cookie session; the frontend neither holds nor signs any token. It does not: render pages, define routes, sign credentials, or serve mock data.

## Files

| File | Purpose |
|---|---|
| `api.ts` | Domain hooks (useSpaces / useSpaceMembers / useSpaceProjects / useCreateSpace / useUpdateSpace / useArchiveSpace / useUpdateSpaceMember) |
| `use-space-events.ts` | `parseSSEFrames` (pure) + `useSpaceEvents` (fetch stream subscription and invalidation) |
| `create-space-dialog.tsx` | Create-space dialog: slug normalized to lowercase, reports the slug for navigation |
| `spaces-page.tsx` | Space management page: lists joined spaces, creates, archives, subscribes to a selected space's events |
| `spaces.test.tsx` | Tests for the behaviors above |

## Dependencies and consumers

Depends on: `src/api` (generated client), TanStack Query.

May be consumed by: routing and layout (`routes.tsx`, `app-sidebar`).

## Invariants

- Every cloud query is gated on `tenantId` (`enabled: !!tenantId`); no request fires without a tenant;
- SSE events only trigger invalidation; authoritative state always comes from REST refetches;
- `parseSSEFrames` is pure: malformed frames are skipped, never thrown.

## Testing

`spaces.test.tsx` mocks the real API with dynamic MSW handlers; tests make no network calls.