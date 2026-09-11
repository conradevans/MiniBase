import { describe, expect, test } from 'vitest'

import {
  CONVERT_APP_PROMPT,
  NEW_APP_PROMPT,
} from './minibasePrompts'

describe('MiniBase AI integration prompts', () => {
  test.each([
    ['new app', NEW_APP_PROMPT],
    ['conversion', CONVERT_APP_PROMPT],
  ])('%s prompt preserves the operational contract without real secrets', (_, prompt) => {
    expect(prompt).toContain('PostgreSQL')
    expect(prompt).toContain('DATABASE_URL')
    expect(prompt).toContain('MiniDeploy')
    expect(prompt).toContain('reactorlab:migrate')
    expect(prompt).toContain('private')
    expect(prompt).toContain('health endpoint')
    expect(prompt).toContain('Do not deploy')
    expect(prompt).not.toMatch(/postgres(?:ql)?:\/\/[^<\s]+:[^<\s]+@/i)
  })
})
