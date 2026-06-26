import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock fetch globally
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

// Mock localStorage
const localStorageMock = (() => {
  let store: Record<string, string> = {}
  return {
    getItem: (key: string) => store[key] || null,
    setItem: (key: string, value: string) => { store[key] = value },
    removeItem: (key: string) => { delete store[key] },
    clear: () => { store = {} },
  }
})()
vi.stubGlobal('localStorage', localStorageMock)

// Mock window.location
vi.stubGlobal('window', { location: { href: '/' } })

// Import after mocks are set up
const { api } = await import('../client')

describe('API Client', () => {
  beforeEach(() => {
    mockFetch.mockReset()
    localStorageMock.clear()
  })

  describe('login', () => {
    it('sends POST to /api/v1/auth/login', async () => {
      mockFetch.mockResolvedValueOnce({
        json: () => Promise.resolve({ token: 'abc', user_id: 1, username: 'admin' }),
        status: 200,
      })

      const result = await api.login('admin', 'password')
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/auth/login',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ username: 'admin', password: 'password' }),
        })
      )
      expect(result.token).toBe('abc')
    })
  })

  describe('getChannels', () => {
    it('sends GET to /api/v1/channels with pagination', async () => {
      mockFetch.mockResolvedValueOnce({
        json: () => Promise.resolve({ data: [], total: 0 }),
        status: 200,
      })

      await api.getChannels(2, 10)
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/channels?page=2&page_size=10',
        expect.objectContaining({ headers: expect.any(Object) })
      )
    })
  })

  describe('getWallet', () => {
    it('sends GET to /api/v1/wallet', async () => {
      mockFetch.mockResolvedValueOnce({
        json: () => Promise.resolve({ balance_cents: 10000 }),
        status: 200,
      })

      await api.getWallet()
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/wallet',
        expect.any(Object)
      )
    })
  })

  describe('topUp', () => {
    it('sends POST to /api/v1/wallet/topup', async () => {
      mockFetch.mockResolvedValueOnce({
        json: () => Promise.resolve({ message: 'Top-up successful' }),
        status: 200,
      })

      await api.topUp({ amount_cents: 5000 })
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/wallet/topup',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ amount_cents: 5000 }),
        })
      )
    })
  })

  describe('authorization header', () => {
    it('includes token when present', async () => {
      localStorageMock.setItem('token', 'my-token')
      mockFetch.mockResolvedValueOnce({
        json: () => Promise.resolve({}),
        status: 200,
      })

      await api.getWallet()
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/wallet',
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer my-token',
          }),
        })
      )
    })

    it('does not include Authorization when no token', async () => {
      mockFetch.mockResolvedValueOnce({
        json: () => Promise.resolve({}),
        status: 200,
      })

      await api.getWallet()
      const callArgs = mockFetch.mock.calls[0]
      const headers = callArgs[1].headers
      expect(headers.Authorization).toBeUndefined()
    })
  })

  describe('getLogs with filters', () => {
    it('encodes filter parameters', async () => {
      mockFetch.mockResolvedValueOnce({
        json: () => Promise.resolve({ data: [], total: 0 }),
        status: 200,
      })

      await api.getLogs(1, 20, { model: 'gpt-4o', provider: 'openai' })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toContain('model=gpt-4o')
      expect(url).toContain('provider=openai')
    })
  })
})
