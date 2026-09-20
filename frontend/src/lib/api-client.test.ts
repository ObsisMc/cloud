import { CanceledError } from 'axios'
import { describe, expect, it } from 'vitest'
import { installFakeHttp } from '@/test/http'
import { customInstance } from './api-client'

describe('customInstance', () => {
  it('unwraps the response body', async () => {
    installFakeHttp({ status: 'ok' })

    await expect(customInstance({ url: '/healthz', method: 'GET' })).resolves.toEqual({
      status: 'ok',
    })
  })

  it('aborts the request when cancel() is called', async () => {
    const http = installFakeHttp({})

    const promise = customInstance({ url: '/healthz', method: 'GET' })
    promise.cancel()

    await expect(promise).rejects.toBeInstanceOf(CanceledError)
    expect(http.requests[0]?.signal?.aborted).toBe(true)
  })

  it('aborts the request when the caller signal aborts', async () => {
    const http = installFakeHttp({})
    const caller = new AbortController()

    const promise = customInstance({ url: '/healthz', method: 'GET', signal: caller.signal })
    caller.abort()

    await expect(promise).rejects.toBeInstanceOf(CanceledError)
    expect(http.requests[0]?.signal?.aborted).toBe(true)
  })
})
