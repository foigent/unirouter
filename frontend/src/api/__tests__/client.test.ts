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

    it('adds project_id for project-scoped usage queries', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getLogs(2, 50, { project_id: 7, request_id: 'req-001' })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toContain('/api/v1/logs?')
      expect(url).toContain('page=2')
      expect(url).toContain('page_size=50')
      expect(url).toContain('project_id=7')
      expect(url).toContain('request_id=req-001')
    })
  })

  describe('project developer APIs', () => {
    it('filters keys by project_id', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getKeys(1, 20, { project_id: 7 })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toBe('/api/v1/keys?page=1&page_size=20&project_id=7')
    })

    it('loads accessible projects', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getProjects(1, 100, { status: 'active' })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toBe('/api/v1/projects?page=1&page_size=100&status=active')
    })

    it('loads approved catalog by project_id', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getModelCatalog(1, 100, { project_id: 7 })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toBe('/api/v1/model-catalog?page=1&page_size=100&project_id=7')
    })

    it('loads and replaces project model sets', async () => {
      mockJsonResponse({ data: [], total: 0 })
      await api.getProjectModelSets(7)
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/projects/7/model-sets')

      mockJsonResponse({ success: true })
      await api.replaceProjectModelSets(7, [1, 2])
      expect(mockFetch.mock.calls[1][0]).toBe('/api/v1/projects/7/model-sets')
      expect(mockFetch.mock.calls[1][1]).toEqual(expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ model_set_ids: [1, 2] }),
      }))
    })
  })

  describe('model governance APIs', () => {
    it('loads model registry with filters', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getModelRegistry(2, 50, { lifecycle: 'ga', approved_only: true, q: 'chat' })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toBe('/api/v1/model-registry?page=2&page_size=50&lifecycle=ga&approved_only=true&q=chat')
    })

    it('creates and updates aliases', async () => {
      mockJsonResponse({ id: 1 })
      await api.createModelAlias({ alias: 'company-chat', target_model: 'gpt-4o' })
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/model-aliases')

      mockJsonResponse({ success: true })
      await api.updateModelAlias(1, { status: 'disabled' })
      expect(mockFetch.mock.calls[1][0]).toBe('/api/v1/model-aliases/1')
      expect(mockFetch.mock.calls[1][1]).toEqual(expect.objectContaining({ method: 'PUT' }))
    })

    it('loads and replaces model set items', async () => {
      mockJsonResponse({ data: [], total: 0 })
      await api.getModelSetItems(3)
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/model-sets/3/items')

      mockJsonResponse({ success: true })
      await api.replaceModelSetItems(3, { items: [{ item_type: 'alias', item_name: 'company-chat' }] })
      expect(mockFetch.mock.calls[1][0]).toBe('/api/v1/model-sets/3/items')
      expect(mockFetch.mock.calls[1][1]).toEqual(expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ items: [{ item_type: 'alias', item_name: 'company-chat' }] }),
      }))
    })
  })

  describe('license APIs', () => {
    it('loads and updates offline license', async () => {
      mockJsonResponse({ status: 'missing' })
      await api.getLicense()
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/license')

      const license = { payload: { customer: 'Acme' }, signature: 'sig' }
      mockJsonResponse({ status: 'active' })
      await api.updateLicense(license)
      expect(mockFetch.mock.calls[1][0]).toBe('/api/v1/license')
      expect(mockFetch.mock.calls[1][1]).toEqual(expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ license }),
      }))
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
