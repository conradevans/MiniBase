const safeMessages = {
  invalid_display_name: 'Enter a database name between 1 and 200 characters.',
  database_attached: 'Detach this database from MiniDeploy before deleting it.',
  database_unavailable: 'This database is not currently available for deletion.',
  deletion_failed: 'MiniBase could not delete the database.',
  provisioning_failed: 'MiniBase could not provision the database.',
  minideploy_unavailable: 'MiniDeploy is temporarily unavailable.',
  database_lifecycle_conflict: 'The database attachment changed. Refresh and try again.',
  database_not_attached: 'This database is not currently attached to MiniDeploy.',
  deployment_not_found: 'That MiniDeploy deployment no longer exists.',
  deployment_unsupported: 'That deployment does not support MiniBase.',
  deployment_attached: 'That deployment already has a database attached.',
  deployment_unavailable: 'That deployment is not currently available for database attachment.',
  lifecycle_inconsistent: 'MiniBase could not verify the final attachment state.',
  invalid_deployment: 'Choose a valid MiniDeploy deployment.',
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
