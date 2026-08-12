import { coreApiURL } from './core-config'

interface SessionResponse {
  token: string
}

let sessionPromise: Promise<string> | undefined
let sessionToken: string | undefined

export const authenticatedCoreFetch: typeof fetch = async (input, init) => {
  const token = await getSessionToken()
  const response = await fetchWithToken(input, init, token)
  if (response.status !== 401) return response

  if (sessionToken === token) {
    sessionPromise = undefined
    sessionToken = undefined
  }
  return fetchWithToken(input, init, await getSessionToken())
}

async function getSessionToken() {
  sessionPromise ??= createSession()
  try {
    return await sessionPromise
  } catch (error) {
    sessionPromise = undefined
    throw error
  }
}

async function createSession() {
  const response = await fetch(coreApiURL('/auth/session'), {
    method: 'POST',
    headers: { Accept: 'application/json' },
  })
  if (!response.ok) throw new Error(`本地 Warmmo Core 会话创建失败：HTTP ${response.status}`)

  const body = await response.json() as Partial<SessionResponse>
  if (typeof body.token !== 'string' || body.token === '') {
    throw new Error('本地 Warmmo Core 返回了无效会话')
  }
  sessionToken = body.token
  return sessionToken
}

function fetchWithToken(input: RequestInfo | URL, init: RequestInit | undefined, token: string) {
  const headers = new Headers(input instanceof Request ? input.headers : undefined)
  for (const [name, value] of new Headers(init?.headers)) headers.set(name, value)
  headers.set('Authorization', `Bearer ${token}`)
  return fetch(input, { ...init, headers })
}
