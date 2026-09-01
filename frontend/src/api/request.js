const safeMessages = {
  invalid_display_name: 'Enter a database name between 1 and 200 characters.',
  provisioning_failed: 'MiniBase could not provision the database.',
  request_too_large: 'The request was too large.',
  service_unavailable: 'MiniBase is temporarily unavailable.',
}

export class MiniBaseApiError extends Error {
  constructor(message, status = 0) {
    super(message)
    this.name = 'MiniBaseApiError'
    this.status = status
  }
}

export async function requestJSON(path, options = {}) {
  let response
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      ...options,
    })
  } catch {
    throw new MiniBaseApiError('Unable to reach MiniBase.')
  }

  const text = await response.text()
  if (!response.ok) {
    let code = ''
    try {
      const body = JSON.parse(text)
      if (typeof body?.error?.code === 'string') {
        code = body.error.code
      }
    } catch {
      code = ''
    }
    throw new MiniBaseApiError(
      safeMessages[code] || `MiniBase request failed with HTTP ${response.status}.`,
      response.status,
    )
  }

  if (!text) {
    return null
  }
  try {
    return JSON.parse(text)
  } catch {
    throw new MiniBaseApiError('MiniBase returned an unexpected response.')
  }
}

export function safeErrorMessage(error, fallback) {
  return error instanceof MiniBaseApiError ? error.message : fallback
}
