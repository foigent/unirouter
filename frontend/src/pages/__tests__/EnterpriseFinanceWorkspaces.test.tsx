import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { buildUTCOverridePriceBands, buildUTCValleyPriceBands, EnterpriseRateCardWorkspace } from '../EnterpriseFinanceWorkspaces';

const apiMocks = vi.hoisted(() => ({
  getEnterpriseRateCards: vi.fn(),
  createEnterpriseRateCardDraft: vi.fn(),
  approveEnterpriseRateCard: vi.fn(),
  activateEnterpriseRateCard: vi.fn(),
  getEnterprisePricingSnapshot: vi.fn(),
  bootstrapLegacyEnterpriseRateCardDrafts: vi.fn(),
  preflightEnterpriseRateCardAuthority: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: apiMocks }));

describe('EnterpriseRateCardWorkspace request-pricing authority', () => {
  beforeEach(() => {
    Object.values(apiMocks).forEach(mock => mock.mockReset());
    apiMocks.createEnterpriseRateCardDraft.mockResolvedValue({});
    apiMocks.bootstrapLegacyEnterpriseRateCardDrafts.mockResolvedValue({
      created_draft_count: 2, existing_draft_count: 1, skipped_model_count: 1,
      skipped_models: ['unknown-model:provider_ref_missing'], state: 'drafts_only',
      approval_required: true, activation_required: true, legacy_runtime_fallback: false,
    });
    apiMocks.preflightEnterpriseRateCardAuthority.mockResolvedValue({
      ready: true, checked_at: '2026-08-13T00:00:00Z',
      switch_instruction: 'Set REQUEST_PRICING_AUTHORITY=rate_card only when ready=true, then restart UniRouter.',
      models: [{
        model_ref: 'deepseek-v4-pro', state: 'ready', rate_card_ref: 'rate-1',
        rate_card_version: 3, layer: 'supplier_contracted', provider_ref: 'deepseek',
        pricing_version: 'rcq2_hash',
      }],
    });
    apiMocks.getEnterpriseRateCards.mockResolvedValue({
      data: [], total: 0,
      request_pricing_authority: {
        authority: 'versioned_rate_card', selection_key: 'model_ref',
        contract_priority: ['supplier_contracted', 'provider_list'], state: 'no_active_rate',
        missing_behavior: 'fail_closed', provider_ambiguity: 'fail_closed',
        active_card_count: 0, draft_card_count: 0, source_evidence_count: 0,
        stale_source_count: 0, official_sync_mode: 'evidence_backed_draft_import',
        active_on_import: false, legacy_compatibility: 'explicit_draft_bootstrap_only',
        public_source_failure: 'keep_current_active_rate', current_mode: 'legacy', strict_active: false,
      },
    });
    apiMocks.approveEnterpriseRateCard.mockResolvedValue({});
    apiMocks.activateEnterpriseRateCard.mockResolvedValue({});
  });

  it('states the fail-closed authority and evidence-backed draft boundary', async () => {
    render(<EnterpriseRateCardWorkspace orgId={9} canManage={false} />);

    expect(await screen.findByText('尚无版本化 Rate Card')).toBeTruthy();
    expect(screen.getByText(/当前仍是旧价格兼容模式/)).toBeTruthy();
    expect(screen.getByText(/所谓官方价格同步是受控导入/)).toBeTruthy();
    expect(screen.getByText(/逐模型预检确认完整性/)).toBeTruthy();
    expect(screen.getByText('旧价格兼容模式')).toBeTruthy();
  });

  it('creates a complete four-meter draft without asking for an internal ref', async () => {
    render(<EnterpriseRateCardWorkspace orgId={9} canManage />);
    await screen.findByText('尚无版本化 Rate Card');
    fireEvent.click(screen.getByText('新建 Rate Card'));

    expect(screen.queryByLabelText('Rate Card 标识')).toBeNull();
    fireEvent.change(screen.getByLabelText('Rate Card 供应商'), { target: { value: 'deepseek' } });
    fireEvent.change(screen.getByLabelText('Rate Card 模型'), { target: { value: 'deepseek-v4-pro' } });
    fireEvent.change(screen.getByLabelText('Rate Card 输入 Token每百万价格'), { target: { value: '1.25' } });
    fireEvent.change(screen.getByLabelText('Rate Card 输出 Token每百万价格'), { target: { value: '2.5' } });
    fireEvent.change(screen.getByLabelText('Rate Card 缓存读取 Token每百万价格'), { target: { value: '0.25' } });
    fireEvent.change(screen.getByLabelText('Rate Card 缓存创建 / 写入 Token每百万价格'), { target: { value: '1.25' } });
    fireEvent.change(screen.getByLabelText('Rate Card 生效日期'), { target: { value: '2026-08-13' } });
    fireEvent.click(screen.getByText('保存草稿'));

    await waitFor(() => expect(apiMocks.createEnterpriseRateCardDraft).toHaveBeenCalledTimes(1));
    const payload = apiMocks.createEnterpriseRateCardDraft.mock.calls[0][0];
    expect(payload.rate_card_ref).toBeUndefined();
    expect(payload.org_id).toBe(9);
    expect(payload.model_ref).toBe('deepseek-v4-pro');
    expect(payload.components.map((item: any) => item.meter)).toEqual([
      'input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_creation_tokens',
    ]);
    expect(payload.components.map((item: any) => item.unit_quantity)).toEqual([
      '1000000', '1000000', '1000000', '1000000',
    ]);
    expect(payload.components.map((item: any) => item.amount.value_minor)).toEqual([
      '1250000', '2500000', '250000', '1250000',
    ]);
    expect(payload.daily_price_bands).toEqual([]);
  });

  it('materializes one explicit UTC valley input as a complete gap-free daily schedule', () => {
    const base = [
      ['input_tokens', '1000000'], ['output_tokens', '2000000'],
      ['cache_read_tokens', '250000'], ['cache_creation_tokens', '1250000'],
    ].map(([meter, value_minor]) => ({
      meter, unit_quantity: '1000000', amount: { value_minor, currency: 'CNY', scale: 1_000_000 },
    }));
    const bands = buildUTCValleyPriceBands({
      price_schedule: 'utc_valley', valley_start_utc: '01:30', valley_end_utc: '06:00',
      valley_input_price_yuan: '0.5', valley_output_price_yuan: '1',
      valley_cache_read_price_yuan: '0.1', valley_cache_creation_price_yuan: '0.6',
    }, base);

    expect(bands?.map(band => [band.band_ref, band.start_minute_utc, band.end_minute_utc])).toEqual([
      ['base-before', 0, 90], ['valley', 90, 360], ['base-after', 360, 1440],
    ]);
    expect(bands?.every((band, index) => index === 0 || band.start_minute_utc === bands[index - 1].end_minute_utc)).toBe(true);
    expect(bands?.[1].components.map(component => component.amount.value_minor)).toEqual([
      '500000', '1000000', '100000', '600000',
    ]);
    expect(buildUTCValleyPriceBands({
      price_schedule: 'utc_valley', valley_start_utc: '22:00', valley_end_utc: '06:00',
    }, base)).toBeNull();
  });

  it('materializes multiple disjoint UTC overrides and fills every base-price gap', () => {
    const component = (meter: string, value_minor: string) => ({
      meter, unit_quantity: '1000000', amount: { value_minor, currency: 'CNY', scale: 1_000_000 },
    });
    const base = [component('input_tokens', '1500000'), component('output_tokens', '4500000'),
      component('cache_read_tokens', '50000'), component('cache_creation_tokens', '1500000')];
    const override = (start_utc: string, end_utc: string, input: string) => ({
      start_utc, end_utc, input_price_yuan: input, output_price_yuan: '9',
      cache_read_price_yuan: '0.10', cache_creation_price_yuan: '3',
    });

    const bands = buildUTCOverridePriceBands([
      override('01:00', '04:00', '3'),
      override('06:00', '10:00', '3'),
    ], base);

    expect(bands?.map(band => [band.start_minute_utc, band.end_minute_utc])).toEqual([
      [0, 60], [60, 240], [240, 360], [360, 600], [600, 1440],
    ]);
    expect(bands?.[1].components.map(item => item.amount.value_minor)).toEqual([
      '3000000', '9000000', '100000', '3000000',
    ]);
    expect(buildUTCOverridePriceBands([
      override('01:00', '04:00', '3'),
      override('03:00', '05:00', '3'),
    ], base)).toBeNull();
  });

  it('keeps legacy bootstrap draft-only and reveals the switch instruction only after ready preflight', async () => {
    render(<EnterpriseRateCardWorkspace orgId={9} canManage />);
    await screen.findByText('尚无版本化 Rate Card');

    fireEvent.click(screen.getByText('从旧价格生成草稿'));
    expect(screen.getByText(/不会批准、激活、切换请求定价权威/)).toBeTruthy();
    fireEvent.click(screen.getByText('确认只生成草稿'));
    await waitFor(() => expect(apiMocks.bootstrapLegacyEnterpriseRateCardDrafts).toHaveBeenCalledWith({ org_id: 9 }));
    expect(await screen.findByText(/迁移草稿：新建 2，已存在 1，跳过 1/)).toBeTruthy();
    expect(screen.getByText(/仍须独立复核和激活/)).toBeTruthy();

    expect(screen.queryByText('REQUEST_PRICING_AUTHORITY=rate_card')).toBeNull();
    fireEvent.change(screen.getByLabelText('Rate Card 预检模型列表'), { target: { value: 'deepseek-v4-pro；deepseek-v4-pro' } });
    fireEvent.click(screen.getByText('逐模型预检'));
    await waitFor(() => expect(apiMocks.preflightEnterpriseRateCardAuthority).toHaveBeenCalledWith({
      org_id: 9, models: [{ model_ref: 'deepseek-v4-pro' }],
    }));
    expect(await screen.findByText('supplier_contracted · deepseek · v3')).toBeTruthy();
    expect(screen.getByText('REQUEST_PRICING_AUTHORITY=rate_card')).toBeTruthy();
    expect(screen.getByText(/此页面不会自动修改环境或服务/)).toBeTruthy();
  });

  it('requires an explicit exact active-card confirmation before replacing a rate', async () => {
    const card = (rate_card_ref: string, version: number, state: string, input: string) => ({
      id: version,
      card: {
        rate_card_ref, provider_ref: 'deepseek', model_ref: 'deepseek-v4-pro',
        layer: 'supplier_contracted', version, state,
        effective_from: '2020-01-01T00:00:00Z', created_by: 'user:1',
        approved_by: 'user:2',
        components: [{
          meter: 'input_tokens', unit_quantity: '1000000',
          amount: { value_minor: input, currency: 'CNY', scale: 1000000 },
        }],
      },
    });
    apiMocks.getEnterpriseRateCards.mockResolvedValue({
      data: [card('old-rate', 3, 'active', '1000000'), card('new-rate', 2, 'approved', '1200000')],
      total: 2,
      request_pricing_authority: {
        authority: 'versioned_rate_card', selection_key: 'model_ref',
        contract_priority: ['supplier_contracted', 'provider_list'], state: 'active_rate_present',
        missing_behavior: 'fail_closed', provider_ambiguity: 'fail_closed',
        active_card_count: 1, draft_card_count: 0, source_evidence_count: 0,
        stale_source_count: 0, official_sync_mode: 'evidence_backed_draft_import',
        active_on_import: false, legacy_compatibility: 'explicit_draft_bootstrap_only',
        public_source_failure: 'keep_current_active_rate', current_mode: 'rate_card', strict_active: true,
      },
    });

    render(<EnterpriseRateCardWorkspace orgId={9} canManage />);
    fireEvent.click(await screen.findByText('替换当前费率'));
    expect(screen.getByText(/将精确替换 old-rate v3/)).toBeTruthy();
    expect(screen.getByText('当前生效价格')).toBeTruthy();
    expect(screen.getByText('替换后价格')).toBeTruthy();
    expect(screen.getByText('input_tokens：¥1.000000 / 1000000 tokens')).toBeTruthy();
    expect(screen.getByText('input_tokens：¥1.200000 / 1000000 tokens')).toBeTruthy();
    fireEvent.click(screen.getByText('确认原子替换'));

    await waitFor(() => expect(apiMocks.activateEnterpriseRateCard).toHaveBeenCalledWith('new-rate', {
      org_id: 9,
      expected_version: 2,
      supersedes_rate_card_ref: 'old-rate',
      expected_superseded_version: 3,
    }));
  });

  it('shows the complete candidate detail before first activation', async () => {
    apiMocks.getEnterpriseRateCards.mockResolvedValue({
      data: [{
        id: 2,
        card: {
          rate_card_ref: 'first-rate', provider_ref: 'deepseek', model_ref: 'deepseek-v4-pro',
          layer: 'supplier_contracted', version: 2, state: 'approved',
          effective_from: '2020-01-01T00:00:00Z', created_by: 'user:1', approved_by: 'user:2',
          components: [{
            meter: 'input_tokens', unit_quantity: '1000000',
            amount: { value_minor: '1000000', currency: 'CNY', scale: 1_000_000 },
          }],
        },
      }],
      total: 1,
      request_pricing_authority: { current_mode: 'legacy', strict_active: false, source_evidence_count: 0, stale_source_count: 0 },
    });

    render(<EnterpriseRateCardWorkspace orgId={9} canManage />);
    fireEvent.click(await screen.findByText('激活'));
    expect(screen.getByText(/将使 first-rate v2 进入 active/)).toBeTruthy();
    expect(screen.getByText('input_tokens：¥1.000000 / 1000000 tokens')).toBeTruthy();
    expect(apiMocks.activateEnterpriseRateCard).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText('确认激活此版本'));
    await waitFor(() => expect(apiMocks.activateEnterpriseRateCard).toHaveBeenCalledWith('first-rate', {
      org_id: 9, expected_version: 2,
    }));
  });

  it('requires the checker to review all UTC prices and source evidence before approval', async () => {
    const component = (meter: string, value_minor: string) => ({
      meter, unit_quantity: '1000000', amount: { value_minor, currency: 'CNY', scale: 1_000_000 },
    });
    const base = [component('input_tokens', '1000000'), component('output_tokens', '2000000'),
      component('cache_read_tokens', '250000'), component('cache_creation_tokens', '1250000')];
    const valley = [component('input_tokens', '500000'), component('output_tokens', '1000000'),
      component('cache_read_tokens', '100000'), component('cache_creation_tokens', '600000')];
    apiMocks.getEnterpriseRateCards.mockResolvedValue({
      data: [{
        id: 7,
        card: {
          rate_card_ref: 'official-rate', provider_ref: 'deepseek', model_ref: 'deepseek-v4-pro',
          layer: 'provider_list', version: 1, state: 'draft', effective_from: '2026-08-13T00:00:00Z',
          source_evidence_ref: 'source-1', created_by: 'user:1', components: base,
          daily_price_bands: [
            { band_ref: 'base-before', start_minute_utc: 0, end_minute_utc: 90, components: base },
            { band_ref: 'valley', start_minute_utc: 90, end_minute_utc: 360, components: valley },
            { band_ref: 'base-after', start_minute_utc: 360, end_minute_utc: 1440, components: base },
          ],
        },
        source_evidence: {
          evidence_ref: 'source-1', source_kind: 'official_import',
          source_url: 'https://provider.example/pricing', source_revision: 'revision-7',
          fetched_at: '2026-08-13T00:00:00Z',
          payload_sha256: `sha256:${'a'.repeat(64)}`, transform_version: 'official-rate-import.v1',
          last_success_at: '2026-08-13T00:00:00Z', stale_after: '2026-08-20T00:00:00Z',
        },
      }],
      total: 1,
      request_pricing_authority: {
        authority: 'versioned_rate_card', selection_key: 'model_ref', contract_priority: ['supplier_contracted', 'provider_list'],
        state: 'no_active_rate', missing_behavior: 'fail_closed', provider_ambiguity: 'fail_closed',
        active_card_count: 0, draft_card_count: 1, source_evidence_count: 1, stale_source_count: 0,
        current_mode: 'legacy', strict_active: false,
      },
    });

    render(<EnterpriseRateCardWorkspace orgId={9} canManage />);
    fireEvent.click(await screen.findByText('复核'));

    expect(screen.getByText(/批准 official-rate v1/)).toBeTruthy();
    expect(screen.getByText('valley · 01:30–06:00')).toBeTruthy();
    expect(screen.getByText('URL：https://provider.example/pricing')).toBeTruthy();
    expect(screen.getByText('修订：revision-7')).toBeTruthy();
    expect(screen.getByText(`原始载荷：sha256:${'a'.repeat(64)}`)).toBeTruthy();
    expect(apiMocks.approveEnterpriseRateCard).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText('确认批准此版本'));
    await waitFor(() => expect(apiMocks.approveEnterpriseRateCard).toHaveBeenCalledWith('official-rate', {
      org_id: 9, expected_version: 1,
    }));
  });
});
