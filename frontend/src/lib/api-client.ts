import Axios, { type AxiosError, type AxiosRequestConfig } from 'axios'

export const AXIOS_INSTANCE = Axios.create({ baseURL: '' })

export const customInstance = <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig,
): Promise<T> => {
  const controller = new AbortController()
  const promise = AXIOS_INSTANCE({
    ...config,
    ...options,
    signal: controller.signal,
  }).then(({ data }) => data)

  // @ts-expect-error orval-generated hooks call .cancel() on the returned promise
  promise.cancel = () => {
    controller.abort()
  }

  return promise
}

export default customInstance

export type ErrorType<Error> = AxiosError<Error>
