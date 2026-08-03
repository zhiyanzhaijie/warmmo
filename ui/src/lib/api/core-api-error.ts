export class CoreApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
    options?: ErrorOptions,
  ) {
    super(message, options)
    this.name = 'CoreApiError'
  }
}
