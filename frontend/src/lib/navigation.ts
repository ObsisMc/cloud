/**
 * The one place the app touches other origins: leaving for the login
 * provider, or opening a provider page in a new tab. Wrapping `window` here
 * keeps components free of the window boundary and gives tests a double:
 * jsdom does not let `location.assign` be spied on and has no `window.open`.
 */

type ExternalNavigator = (url: string) => void

let navigator: ExternalNavigator = (url) => window.location.assign(url)

// `noopener` severs the new tab from this one, so the other origin can never reach back into
// the app through `window.opener`.
let opener: ExternalNavigator = (url) => {
  window.open(url, '_blank', 'noopener,noreferrer')
}

/** Sends the browser to `url`, leaving this app. Never returns to the caller in a real browser. */
export function navigateExternal(url: string): void {
  navigator(url)
}

/** Opens `url` in a new tab, keeping this tab and its state; the app continues here. */
export function openExternalTab(url: string): void {
  opener(url)
}

/**
 * Replaces the browser navigation with `next` and returns the function that
 * restores the previous behavior. Test scaffolding uses it; production code
 * must not.
 */
export function replaceExternalNavigation(next: ExternalNavigator): () => void {
  const previous = navigator
  navigator = next
  return () => {
    navigator = previous
  }
}

/** Same as {@link replaceExternalNavigation}, for the new-tab opener. */
export function replaceExternalTabOpener(next: ExternalNavigator): () => void {
  const previous = opener
  opener = next
  return () => {
    opener = previous
  }
}
