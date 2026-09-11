import { useEffect, useRef, useState } from 'react'

import {
  CONVERT_APP_PROMPT,
  NEW_APP_PROMPT,
} from '../minibasePrompts'

const options = [
  {
    id: 'new',
    title: 'New App',
    description: 'Building something new? Give this prompt to your coding assistant so the application is structured for MiniBase from the start.',
    prompt: NEW_APP_PROMPT,
  },
  {
    id: 'convert',
    title: 'Convert Existing App',
    description: 'Already using MongoDB, MySQL, or another database? Give this prompt to your coding assistant to refactor the application for MiniBase.',
    prompt: CONVERT_APP_PROMPT,
  },
]

export default function AIIntegration() {
  const [copied, setCopied] = useState('')
  const resetTimer = useRef()

  useEffect(() => {
    const timerRef = resetTimer
    return () => window.clearTimeout(timerRef.current)
  }, [])

  async function copyPrompt(option) {
    try {
      await navigator.clipboard.writeText(option.prompt)
      window.clearTimeout(resetTimer.current)
      setCopied(option.id)
      resetTimer.current = window.setTimeout(() => {
        setCopied('')
      }, 3000)
    } catch {
      setCopied('')
    }
  }

  return (
    <section className="ai-integration" aria-labelledby="ai-integration-title">
      <div className="ai-integration-heading">
        <div>
          <p className="eyebrow">APPLICATION PREPARATION</p>
          <h2 id="ai-integration-title">AI Integration</h2>
        </div>
        <p>
          Copy a repository-safe prompt that explains MiniBase's real
          MiniDeploy connection and migration contract.
        </p>
      </div>

      <div className="prompt-option-grid">
        {options.map((option) => (
          <article className="prompt-option" key={option.id}>
            <div>
              <h3>{option.title}</h3>
              <p>{option.description}</p>
            </div>
            <button
              className="button secondary"
              type="button"
              aria-label={'Copy ' + option.title + ' prompt'}
              onClick={() => copyPrompt(option)}
            >
              <span aria-live="polite">
                {copied === option.id ? 'Copied' : 'Copy Prompt'}
              </span>
            </button>
          </article>
        ))}
      </div>
    </section>
  )
}
