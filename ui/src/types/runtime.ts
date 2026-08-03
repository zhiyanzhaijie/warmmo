export interface RuntimeInfo {
  name: string
  version: string
  status: string
  goVersion: string
  platform: string
  serverTime: string
  requestId: string
}

export type RuntimeRequestState =
  | { status: 'loading' }
  | { status: 'success'; data: RuntimeInfo; latencyMs: number }
  | { status: 'error'; message: string }
