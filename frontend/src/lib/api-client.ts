import { create, isAxiosError, type AxiosError, type AxiosRequestConfig } from 'axios'
import { clearCloudCredentials, getCloudCredentials } from '@/lib/cloud-session'

/**
 * Shared axios instance behind every generated hook in `src/api`.
 *
 * Cross-cutting HTTP policy (base URL, auth headers, interceptors) belongs here
 * so that generated code and hand-written code observe one configuration.
 */
export const AXIOS_INSTANCE = create({ baseURL: '' })

// Attach the dual gateway credentials (service + caller-bound user JWT) to
// every request once the tab has signed in through devgateway. The signing
// keys never enter frontend code; this only replays tokens the gateway issued.
// POST and DELETE additionally receive a fresh idempotency key when the caller
// did not supply one: the cloud core rejects them without it. The interceptor
// is synchronous so axios keeps dispatching to the adapter immediately — an
// abort must still win the race the way it does without interceptors.
AXIOS_INSTANCE.interceptors.request.use(
  (config) => {
    const credentials = getCloudCredentials()
    if (credentials) {
      config.headers.set('Authorization', `Bearer ${credentials.serviceToken}`)
      config.headers.set('X-Ora-User-Token', credentials.userToken)
    }
    if (
      (config.method === 'post' || config.method === 'delete') &&
      !config.headers.get('Idempotency-Key')
    ) {
      config.headers.set('Idempotency-Key', crypto.randomUUID())
    }
    return config
  },
  undefined,
  { synchronous: true },
)

// An expired or rejected credential ends the session so the sign-in flow can
// re-run instead of every subsequent query failing with the same 401.
AXIOS_INSTANCE.interceptors.response.use(undefined, (error) => {
  if (isAxiosError(error) && error.response?.status === 401 && getCloudCredentials()) {
    clearCloudCredentials()
  }
  throw error
})

/**
 * Request shape the orval-generated client passes to {@link customInstance}.
 *
 * orval emits `signal: AbortSignal | undefined` rather than omitting the key,
 * so the type must accept an explicit `undefined` under
 * `exactOptionalPropertyTypes`.
 */
export type RequestConfig = Omit<AxiosRequestConfig, 'signal'> & {
  signal?: AbortSignal | undefined
}

/**
 * Promise returned to generated hooks; orval calls `cancel()` on it when a
 * query is torn down before the request settles.
 */
export type CancellablePromise<T> = Promise<T> & { cancel: () => void }

/**
 * orval mutator: executes one request and unwraps the response body.
 *
 * Cancellation has two sources that must both abort the request: the
 * `AbortSignal` react-query passes in `config`, and the legacy `cancel()`
 * method orval attaches to the returned promise.
 */
export const customInstance = <T>(
  config: RequestConfig,
  options?: AxiosRequestConfig,
): CancellablePromise<T> => {
  const controller = new AbortController()
  const signal =
    config.signal === undefined
      ? controller.signal
      : AbortSignal.any([config.signal, controller.signal])
  const promise = AXIOS_INSTANCE<T>({ ...config, ...options, signal }).then(({ data }) => data)
  return Object.assign(promise, { cancel: () => controller.abort() })
}

/** Error type generated hooks expose; the body is the server's `Fault` contract. */
export type ErrorType<Error> = AxiosError<Error>
