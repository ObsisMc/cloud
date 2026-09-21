/**
 * The one place the app leaves itself for another origin (the login
 * provider). Wrapping `window.location` here keeps components free of the
 * window boundary and gives tests a double: jsdom does not let
 * `location.assign` be spied on.
 */

type ExternalNavigator = (url: string) => void

let navigator: ExternalNavigator = (url) => window.location.assign(url)

/** Sends the browser to `url`, leaving this app. Never returns to the caller in a real browser. */
export function navigateExternal(url: string): void {
  navigator(url)
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
