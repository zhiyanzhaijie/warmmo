const defaultCoreOrigin = 'http://127.0.0.1:8787'

export const coreOrigin = normalizeCoreOrigin(import.meta.env.VITE_CORE_ORIGIN ?? defaultCoreOrigin)

export const coreApiBaseURL = `${coreOrigin}/api/v1`

export function coreApiURL(path: string) {
  return new URL(`.${path}`, `${coreApiBaseURL}/`).toString()
}

function normalizeCoreOrigin(value: string) {
  const url = new URL(value)
  if (url.origin !== 'http://127.0.0.1:8787' && url.origin !== 'http://localhost:8787') {
    throw new Error('VITE_CORE_ORIGIN 必须指向 http://127.0.0.1:8787')
  }
  return url.origin
}
