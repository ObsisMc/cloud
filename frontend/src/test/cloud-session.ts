import { useAuthStore } from '@/state/auth-store'

/**
 * Shared fixtures for tests that exercise the cloud-backed workspace flow.
 * Signing the tab in with {@link setCloudSession} turns the current-space
 * provider's `cloudMode` on, so pages resolve their space through the generated
 * client instead of the mock store. Tests install the matching MSW space
 * handlers themselves.
 */
export const TEST_TENANT_ID = '11111111-1111-1111-1111-111111111111'
export const TEST_SPACE_ID = '22222222-2222-2222-2222-222222222222'

/** Signs the auth store into a cloud session for {@link TEST_TENANT_ID}. */
export function setCloudSession(): void {
  useAuthStore.getState().setSession({
    user: { id: 'u1', displayName: 'Alice', subject: 'alice' },
    tenantId: TEST_TENANT_ID,
    tenantName: 'Acme',
  })
}
