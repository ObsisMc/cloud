import { describe, expect, it } from 'vitest'
import { navigateExternal, replaceExternalNavigation } from './navigation'

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
})
