import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  clearCloudCredentials,
  getCloudCredentials,
  hasCloudSession,
  loginCloud,
  setCloudCredentials,
} from './cloud-session'

const CREDENTIALS = {
  serviceToken: 'svc-token',
  userToken: 'usr-token',
  expiresAt: '2026-09-18T12:00:00+08:00',
}

describe('cloud-session', () => {
  afterEach(() => {
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('stores and reads credentials for the tab lifetime', () => {
    expect(hasCloudSession()).toBe(false)
    setCloudCredentials(CREDENTIALS)
    expect(getCloudCredentials()).toEqual(CREDENTIALS)
    expect(hasCloudSession()).toBe(true)
  })

  it('clears credentials explicitly', () => {
    setCloudCredentials(CREDENTIALS)
    clearCloudCredentials()
    expect(getCloudCredentials()).toBeNull()
  })

  it('treats corrupted storage as no session', () => {
    sessionStorage.setItem('ora-cloud-session', '{broken json')
    expect(getCloudCredentials()).toBeNull()
    expect(hasCloudSession()).toBe(false)
  })

  it('rejects stored entries missing either token', () => {
    sessionStorage.setItem('ora-cloud-session', JSON.stringify({ serviceToken: 'only-one' }))
    expect(getCloudCredentials()).toBeNull()
  })

  it('signs in through devgateway and persists the result', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => CREDENTIALS,
      }),
    )
    const result = await loginCloud('corp', 'alice', 'Alice')
    expect(result).toEqual(CREDENTIALS)
    expect(fetch).toHaveBeenCalledWith(
      '/devgateway/login?source=corp&subject=alice&display=Alice',
      expect.anything(),
    )
    expect(getCloudCredentials()).toEqual(CREDENTIALS)
  })

  it('propagates a failed gateway login without storing anything', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502 }))
    await expect(loginCloud('corp', 'alice', 'Alice')).rejects.toThrow('devgateway login failed')
    expect(getCloudCredentials()).toBeNull()
  })
})
