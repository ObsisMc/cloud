import { onTestFinished } from 'vitest'
import { replaceExternalNavigation, replaceExternalTabOpener } from '@/lib/navigation'

/** Handle returned by {@link installFakeNavigation} for asserting on where the app tried to go. */
export type FakeNavigation = {
  /** External URLs the app navigated this tab to, oldest first. */
  destinations: string[]
  /** External URLs the app opened in a new tab, oldest first. */
  openedTabs: string[]
}

/**
 * Replaces external navigation and new-tab opening so a test can observe
 * the login redirect and provider pages instead of leaving jsdom. Restored
 * when the calling test finishes, so the helper must be called inside a
 * test, not at module scope.
 */
export function installFakeNavigation(): FakeNavigation {
  const destinations: string[] = []
  const openedTabs: string[] = []
  onTestFinished(replaceExternalNavigation((url) => destinations.push(url)))
  onTestFinished(replaceExternalTabOpener((url) => openedTabs.push(url)))
  return { destinations, openedTabs }
}
