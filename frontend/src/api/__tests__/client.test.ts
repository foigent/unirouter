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

    it('loads OIDC public config and builds start URL', async () => {
      mockJsonResponse({ enabled: true, provider_name: 'Acme IDP' })

      const result = await api.getOIDCConfig()
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/auth/oidc/config',
        expect.objectContaining({ headers: expect.any(Object) })
      )
      expect(result.enabled).toBe(true)
      expect(api.getOIDCStartURL('/developer')).toBe('/api/v1/auth/oidc/start?redirect=%2Fdeveloper')
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

  describe('organization model prices', () => {
    it('defaults to the authenticated organization without emitting org_id=1', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getOrgModelPrices(1, 20)

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/org-model-prices?page=1&page_size=20',
        expect.any(Object)
      )
    })

    it('encodes an explicitly selected organization', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getOrgModelPrices(2, 50, 9)

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/org-model-prices?page=2&page_size=50&org_id=9',
        expect.any(Object)
      )
    })

    it('uses the multiplier contract for create and update', async () => {
      mockJsonResponse({ id: 4 })
      await api.createOrgModelPrice({ org_id: 9, model_name: 'gpt-4o', multiplier: 1.25 })
      expect(mockFetch).toHaveBeenLastCalledWith(
        '/api/v1/org-model-prices',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ org_id: 9, model_name: 'gpt-4o', multiplier: 1.25 }),
        })
      )

      mockJsonResponse({ success: true })
      await api.updateOrgModelPrice(4, { multiplier: 1.5 })
      expect(mockFetch).toHaveBeenLastCalledWith(
        '/api/v1/org-model-prices/4',
        expect.objectContaining({ method: 'PUT', body: JSON.stringify({ multiplier: 1.5 }) })
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

    it('adds usage_source for usage-source drilldown', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getLogs(1, 20, { usage_source: 'counter_v2' })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toContain('usage_source=counter_v2')
    })

    it('encodes structural evidence filters', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getLogs(1, 20, {
        trace_id: 'trace-001',
        upstream_request_id: 'upstream-001',
        evidence_ref: 'unirouter:event:1',
        source_event_hash: 'hash-001',
      })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toContain('trace_id=trace-001')
      expect(url).toContain('upstream_request_id=upstream-001')
      expect(url).toContain('evidence_ref=unirouter%3Aevent%3A1')
      expect(url).toContain('source_event_hash=hash-001')
    })

    it('loads request trace for a call log', async () => {
      mockJsonResponse({ request_id: 'req-001', events: [] })

      await api.getLogTrace(31)
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/logs/31/trace')
    })
  })

  describe('PRD5 data exchange control', () => {
    it('uses the approval and exchange ledger routes', async () => {
      mockJsonResponse({ data: [], total: 0 })
      await api.getDataExchangeItems(2, 50, 'approved')
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/data-exchange/items?page=2&page_size=50&status=approved')

      mockJsonResponse({ id: 9 })
      await api.createDataExchangeReceipt(9, { exchange_id: 'uxchg_test_001' })
      expect(mockFetch.mock.calls[1][0]).toBe('/api/v1/data-exchange/exchanges/9/receipts')
      expect(mockFetch.mock.calls[1][1]).toEqual(expect.objectContaining({ method: 'POST' }))

      mockJsonResponse({ members: [], deliveries: [], receipts: [] })
      await api.getDataExchangeActivity(9)
      expect(mockFetch.mock.calls[2][0]).toBe('/api/v1/data-exchange/exchanges/9/activity')

      mockJsonResponse({ id: 9 })
      await api.markDataExchangeArtifactDeleted(9)
      expect(mockFetch.mock.calls[3][0]).toBe('/api/v1/data-exchange/exchanges/9/artifact-delete')
    })
  })

  describe('getReconciliation with filters', () => {
    it('encodes usage_source filter', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getReconciliation(2, 50, { model: 'company-chat', status: 'reconciled', usage_source: 'adapter_estimated' })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toBe('/api/v1/reconciliation?page=2&page_size=50&model=company-chat&status=reconciled&usage_source=adapter_estimated')
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

    it('preserves explicit platform organization selection on global-ID mutations', async () => {
      mockJsonResponse({ success: true })
      await api.updateModelRegistry(1, { lifecycle: 'deprecated' }, 9)
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/model-registry/1?org_id=9')

      mockJsonResponse({ success: true })
      await api.updateModelAlias(2, { status: 'disabled' }, 9)
      expect(mockFetch.mock.calls[1][0]).toBe('/api/v1/model-aliases/2?org_id=9')

      mockJsonResponse({ success: true })
      await api.replaceModelSetItems(3, { items: [] }, 9)
      expect(mockFetch.mock.calls[2][0]).toBe('/api/v1/model-sets/3/items?org_id=9')
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

  describe('notification APIs', () => {
    it('loads audit logs with PRD2 filters', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getAuditLogs(3, 50, {
        org_id: '1',
        project_id: '9',
        actor_id: '7',
        action: 'channel.test',
        resource: 'channel',
        source: 'gateway_api',
        start_time: '2026-07-02T00:00:00Z',
        end_time: '2026-07-02T23:59:59Z',
      })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toBe('/api/v1/audit-logs?page=3&page_size=50&org_id=1&project_id=9&actor_id=7&action=channel.test&resource=channel&source=gateway_api&start_time=2026-07-02T00%3A00%3A00Z&end_time=2026-07-02T23%3A59%3A59Z')
    })

    it('loads notification events with filters', async () => {
      mockJsonResponse({ data: [], total: 0 })

      await api.getNotificationEvents(2, 50, {
        event_type: 'project_budget.threshold',
        severity: 'critical',
        delivery_state: 'failed',
        project_id: 9,
      })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toBe('/api/v1/notification-events?page=2&page_size=50&event_type=project_budget.threshold&severity=critical&delivery_state=failed&project_id=9')
    })

    it('loads delivery state and writes webhook routes', async () => {
      mockJsonResponse({ data: [], total: 0 })
      await api.getNotificationDeliveries(1, 20, { status: 'failed', event_id: 7 })
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/notification-deliveries?page=1&page_size=20&status=failed&event_id=7')

      mockJsonResponse({ id: 3 })
      await api.createNotificationWebhook({ name: 'Ops', url: 'https://ops.example/hook', event_types: ['project_budget.threshold'] })
      expect(mockFetch.mock.calls[1][0]).toBe('/api/v1/notification-webhooks')
      expect(mockFetch.mock.calls[1][1]).toEqual(expect.objectContaining({ method: 'POST' }))

      mockJsonResponse({ success: true })
      await api.updateNotificationWebhook(3, { enabled: false })
      expect(mockFetch.mock.calls[2][0]).toBe('/api/v1/notification-webhooks/3')
      expect(mockFetch.mock.calls[2][1]).toEqual(expect.objectContaining({ method: 'PUT' }))
    })
  })

  describe('PRD2 Batch 23 APIs', () => {
    it('loads usage analytics with drilldown filters', async () => {
      mockJsonResponse({ data: [], summary: {} })

      await api.getUsageAnalytics({
        group_by: 'alias',
        project_id: 7,
        owner_user_id: 42,
        key_type: 'personal',
        scope: 'my',
        model: 'gpt-4o',
        provider: 'openai',
        usage_source: 'counter_v2',
        status: 'success',
      })
      const url = mockFetch.mock.calls[0][0]
      expect(url).toBe('/api/v1/usage/analytics?group_by=alias&project_id=7&owner_user_id=42&key_type=personal&scope=my&model=gpt-4o&provider=openai&status=success&usage_source=counter_v2')
    })

    it('loads internal status and error docs', async () => {
      mockJsonResponse({ status: 'operational', incidents: [] })
      await api.getInternalStatus({ project_id: 7 })
      expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/internal-status?project_id=7')

      mockJsonResponse({ status: 'operational' })
      await api.getRuntimeStatus()
      expect(mockFetch.mock.calls[1][0]).toBe('/api/v1/runtime/status')

      mockJsonResponse({ data: [] })
      await api.getErrorDocs()
      expect(mockFetch.mock.calls[2][0]).toBe('/api/v1/error-docs')
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
