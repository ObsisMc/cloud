# onboarding: the first workspace

[中文](README.md) | [English](README.en.md)

## Responsibility

A signed-in member who has no workspace yet creates their first one here. It is the `/onboarding` route and also where `/` lands: members who already have a workspace are sent straight to it, so this screen only ever serves first use.

The form has a name and a slug (derived from the name until the user edits it) and previews the address. On submit the page picks the API by the member's state: no tenant → `POST /api/v1/tenants` (the backend implicitly provisions a tenant and makes the caller its administrator and the space's owner; the product never shows tenants); a tenant with no live space → `POST /tenants/{tid}/spaces`. Both land on `/{slug}/issues`.

It does not log in (`features/auth`), create later workspaces (the sidebar's `CreateSpaceDialog`) or expose any tenant management.

## Files

| File | Description |
| --- | --- |
| `onboarding-page.tsx` | `OnboardingPage`: loading/error/already-has-workspace redirect and the `CreateFirstWorkspace` form |
| `onboarding-page.test.tsx` | Redirect, slug derivation, both creation paths, fault-code display, invalid-slug disabling, sign-out |

## Dependency direction

Depends on: `features/auth/session` (display name and sign-out), `features/spaces/api` (`useJoinedSpaces` / `useCreateTenant` / `useCreateSpace`), `features/spaces/slug`, `src/lib/paths`, UI components.

May be consumed by: `routes.tsx`.

## Invariants

- The page renders only inside `RequireSession`; it assumes the session is confirmed.
- A member with a workspace never sees the form and is `Navigate`d away, which is what lets `/` redirect here unconditionally.
- The slug must pass `isValidSlug` before submit, the same rule the backend applies; a backend rejection shows its fault code verbatim and keeps the form.

## Testing

`onboarding-page.test.tsx` mounts real routes through `renderRoutes` and uses MSW to distinguish the three preconditions: no tenant, tenant without a space, and an existing space.
