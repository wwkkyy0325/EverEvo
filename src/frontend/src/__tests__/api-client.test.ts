import { describe, it, expect } from 'vitest'
import { ApiError } from '@/api/client'

describe('ApiError', () => {
  it('creates an error with message and code', () => {
    const err = new ApiError('请求超时', 'TIMEOUT')
    expect(err.message).toBe('请求超时')
    expect(err.code).toBe('TIMEOUT')
    expect(err.name).toBe('ApiError')
  })

  it('defaults code to UNKNOWN', () => {
    const err = new ApiError('something')
    expect(err.code).toBe('UNKNOWN')
  })
})
