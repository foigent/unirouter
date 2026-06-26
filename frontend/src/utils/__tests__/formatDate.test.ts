import { describe, it, expect } from 'vitest'
import { formatDateTime } from '../formatDate'

describe('formatDateTime', () => {
  it('returns - for null', () => {
    expect(formatDateTime(null)).toBe('-')
  })

  it('returns - for undefined', () => {
    expect(formatDateTime(undefined)).toBe('-')
  })

  it('returns - for empty string', () => {
    expect(formatDateTime('')).toBe('-')
  })

  it('returns - for invalid date', () => {
    expect(formatDateTime('not-a-date')).toBe('-')
  })

  it('formats valid ISO date', () => {
    const result = formatDateTime('2026-06-25T10:30:45Z')
    // Result depends on local timezone, but should contain the date parts
    expect(result).toMatch(/2026-06-2[45]/)
    expect(result).not.toBe('-')
  })

  it('formats date with milliseconds', () => {
    const result = formatDateTime('2026-01-15T08:00:00.000Z')
    expect(result).toMatch(/2026-01-1[45]/)
  })
})
