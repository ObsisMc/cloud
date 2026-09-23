import {
  AxiosError,
  type AxiosAdapter,
  type AxiosResponse,
  type InternalAxiosRequestConfig,
} from 'axios'
import { onTestFinished } from 'vitest'
import { AXIOS_INSTANCE } from '@/lib/api-client'

/** One request observed by {@link installFakeHttp}, in arrival order. */
export type RecordedRequest = {
  url: string | undefined
  method: string | undefined
  signal: AbortSignal | undefined
  /** Request headers as sent, so policy tests can assert on what left the client. */
  headers: Record<string, unknown>
}

/** Handle returned by {@link installFakeHttp} for asserting on traffic. */
export type FakeHttp = {
  /** Requests received so far, oldest first. */
  requests: RecordedRequest[]
}

// axios types the signal loosely; tests only ever pass real AbortSignals.
const toAbortSignal = (signal: InternalAxiosRequestConfig['signal']): AbortSignal | undefined =>
  signal instanceof AbortSignal ? signal : undefined

/**
 * Replaces the shared axios adapter so tests run without a network.
 *
 * Every request resolves with `body` and HTTP 200 unless `status` is 400 or
 * above, in which case it rejects the way axios does for error statuses. The
 * previous adapter is restored when the calling test finishes, so the helper
 * must be called inside a test, not at module scope.
 */
export function installFakeHttp(body: unknown, status = 200): FakeHttp {
  const requests: RecordedRequest[] = []
  const adapter: AxiosAdapter = (config) => {
    requests.push({
      url: config.url,
      method: config.method,
      signal: toAbortSignal(config.signal),
      headers: config.headers.toJSON(),
    })
    const response: AxiosResponse = { data: body, status, statusText: '', headers: {}, config }
    // axios wraps error statuses in AxiosError; reproduce that so interceptors
    // and `isAxiosError` see the same shape as with the real adapter.
    return status < 400
      ? Promise.resolve(response)
      : Promise.reject(
          new AxiosError(
            `Request failed with status code ${status}`,
            'ERR_BAD_REQUEST',
            config,
            undefined,
            response,
          ),
        )
  }
  const { defaults } = AXIOS_INSTANCE
  const previous = defaults.adapter
  defaults.adapter = adapter
  onTestFinished(() => {
    if (previous === undefined) {
      delete defaults.adapter
    } else {
      defaults.adapter = previous
    }
  })
  return { requests }
}
