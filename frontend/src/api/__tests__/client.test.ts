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
const { api, ApiError } = await import('../client')

function mockJsonResponse(payload: unknown, status = 200) {
  mockFetch.mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(payload)),
  })
}

describe('API Client', () => {
  beforeEach(() => {
    mockFetch.mockReset()
    localStorageMock.clear()
  })

  describe('login', () => {
    it('sends POST to /api/v1/auth/login', async () => {
      mockJsonResponse({ token: 'abc', user_id: 1, username: 'admin' })

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
      mockJsonResponse({ data: [], total: 0 })

      await api.getChannels(2, 10)
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/channels?page=2&page_size=10',
        expect.objectContaining({ headers: expect.any(Object) })
      )
    })
  })

  describe('getWallet', () => {
    it('sends GET to /api/v1/wallet', async () => {
      mockJsonResponse({ balance_cents: 10000 })

      await api.getWallet()
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/wallet',
        expect.any(Object)
      )
    })
  })

  describe('topUp', () => {
    it('sends POST to /api/v1/wallet/topup', async () => {
      mockJsonResponse({ message: 'Top-up successful' })

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

  describe('changePassword', () => {
    it('sends PUT to /api/v1/me/password', async () => {
      mockJsonResponse({ success: true })

      await api.changePassword({ old_password: 'oldpass', new_password: 'newpass123' })
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/me/password',
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify({ old_password: 'oldpass', new_password: 'newpass123' }),
        })
      )
    })
  })

  describe('authorization header', () => {
    it('includes token when present', async () => {
      localStorageMock.setItem('token', 'my-token')
      mockJsonResponse({})

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
      mockJsonResponse({})

      await api.getWallet()
      const callArgs = mockFetch.mock.calls[0]
      const headers = callArgs[1].headers
      expect(headers.Authorization).toBeUndefined()
    })
  })

  describe('getLogs with filters', () => {
    it('encodes filter parameters', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getLogs(1, 20, { model: 'gpt-4o', provider: 'openai' })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toContain('model=gpt-4o')
      expect(url).toContain('provider=openai')
    })
  })

  describe('error handling', () => {
    it('throws ApiError for non-2xx responses', async () => {
      mockJsonResponse({ error: { message: 'invalid amount' } }, 400)

      await expect(api.topUp({ amount_cents: 0 })).rejects.toMatchObject({
        name: 'ApiError',
        status: 400,
        message: 'invalid amount',
      })
    })

    it('clears token and redirects on 401', async () => {
      localStorageMock.setItem('token', 'expired-token')
      mockJsonResponse({ message: 'token expired' }, 401)

      await expect(api.getWallet()).rejects.toBeInstanceOf(ApiError)
      expect(localStorageMock.getItem('token')).toBeNull()
      expect(window.location.href).toBe('/login')
    })
  })
})
