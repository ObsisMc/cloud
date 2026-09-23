import { describe, expect, it } from 'vitest'
import {
  navigateExternal,
  openExternalTab,
  replaceExternalNavigation,
  replaceExternalTabOpener,
} from './navigation'

describe('navigation', () => {
  it('routes external navigation through the replaceable navigator and restores it', () => {
    const seen: string[] = []
    const restore = replaceExternalNavigation((url) => seen.push(url))

    navigateExternal('https://example.com/a')
    expect(seen).toEqual(['https://example.com/a'])

    restore()
    const again: string[] = []
    const restoreAgain = replaceExternalNavigation((url) => again.push(url))
    navigateExternal('https://example.com/b')
    restoreAgain()
    expect(seen).toEqual(['https://example.com/a'])
    expect(again).toEqual(['https://example.com/b'])
  })

  it('keeps the new-tab opener separate from navigation and restores it too', () => {
    const navigated: string[] = []
    const opened: string[] = []
    const restoreNavigation = replaceExternalNavigation((url) => navigated.push(url))
    const restoreOpener = replaceExternalTabOpener((url) => opened.push(url))

    openExternalTab('https://example.com/tab')
    expect(opened).toEqual(['https://example.com/tab'])
    expect(navigated).toEqual([])

    restoreOpener()
    const later: string[] = []
    const restoreLater = replaceExternalTabOpener((url) => later.push(url))
    openExternalTab('https://example.com/later')
    restoreLater()
    restoreNavigation()
    expect(opened).toEqual(['https://example.com/tab'])
    expect(later).toEqual(['https://example.com/later'])
  })
})
