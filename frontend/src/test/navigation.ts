import { onTestFinished } from 'vitest'
import { replaceExternalNavigation } from '@/lib/navigation'

/** Handle returned by {@link installFakeNavigation} for asserting on where the app tried to go. */
export type FakeNavigation = {
  /** External URLs the app navigated to, oldest first. */
  destinations: string[]
}

/**
 * Replaces external navigation so a test can observe the login redirect
 * instead of leaving jsdom. Restored when the calling test finishes, so the
 * helper must be called inside a test, not at module scope.
 */
export function installFakeNavigation(): FakeNavigation {
  const destinations: string[] = []
  onTestFinished(replaceExternalNavigation((url) => destinations.push(url)))
  return { destinations }
}
