export class CoreApiError extends Error {
  readonly status: number
  readonly code?: string
  constructor(
    message: string,
    status: number,
    code?: string,
    options?: ErrorOptions,
  ) {
    super(message, options)
    this.name = 'CoreApiError'
    this.status = status
    this.code = code
  }
}
