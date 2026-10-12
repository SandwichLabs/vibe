import { describe, it, expect } from 'vitest'
import { errorText, isConflict, isDeletedConflict, settingsError } from './errors'

describe('errorText', () => {
  it('strips a triple-wrapped Go error down to the real root cause', () => {
    const e = new Error('failed to run query: query failed: failed to execute duckdb query: Parser Error: syntax error at or near "WHER"')
    expect(errorText(e)).toBe('Parser Error: syntax error at or near "WHER"')
  })

  it('strips "failed to open file: " to reveal the friendly passphrase message', () => {
    const e = new Error("failed to open file: That passphrase didn't work. Check Caps Lock and try again.")
    expect(errorText(e)).toBe("That passphrase didn't work. Check Caps Lock and try again.")
  })

  it('handles a quoted colon inside the wrapper prefix', () => {
    const e = new Error('failed to load query "a:b": not found')
    expect(errorText(e)).toBe('not found')
  })

  it('accepts a plain string', () => {
    expect(errorText('query failed: boom')).toBe('boom')
  })

  it('accepts an Error instance', () => {
    expect(errorText(new Error('plain message'))).toBe('plain message')
  })

  it('leaves a message that is only the wrapper unchanged', () => {
    expect(errorText('query failed: ')).toBe('query failed:')
  })

  it('leaves a message with no wrapper unchanged', () => {
    expect(errorText('no file open')).toBe('no file open')
  })

  it('does not strip "could not" / "unable to" root causes', () => {
    expect(errorText('could not connect to database')).toBe('could not connect to database')
    expect(errorText('unable to read file')).toBe('unable to read file')
  })
})

describe('isConflict', () => {
  it('recognises the bridge conflict marker on an Error or a string', () => {
    expect(isConflict(new Error('conflict: notebooks/a.vnb changed on disk since it was loaded'))).toBe(true)
    expect(isConflict('conflict: x')).toBe(true)
  })
  it('does not match other errors or a mid-message mention', () => {
    expect(isConflict(new Error('failed to save: disk full'))).toBe(false)
    expect(isConflict(new Error('there is a conflict: here'))).toBe(false)
    expect(isConflict(null)).toBe(false)
  })
  it('tells a deleted file from a changed one', () => {
    expect(isDeletedConflict(new Error('conflict: notebooks/a.vnb was deleted on disk since it was loaded'))).toBe(true)
    expect(isDeletedConflict(new Error('conflict: notebooks/a.vnb changed on disk since it was loaded'))).toBe(false)
    expect(isDeletedConflict(new Error('file was deleted'))).toBe(false)
  })
  it('settingsError drops the marker for a conflict', () => {
    expect(settingsError(new Error('conflict: vibe.yaml changed'))).toEqual({ text: 'vibe.yaml changed', stale: true })
  })
})
