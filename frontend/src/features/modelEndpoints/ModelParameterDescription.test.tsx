import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { ModelExecutionProfile } from '../../api/modelExecutionProfiles';
import ModelParameterDescription from './ModelParameterDescription';

const profile = (max: boolean): ModelExecutionProfile => ({
  id: max ? 'qwen.max' : 'qwen.flash', version: '1.0.0', default_output_limit: 8192,
  parameter_controls: {
    version: 'unirouter.parameter-controls.v1',
    reasoning: {
      profile_id: 'exact', mode: 'toggle', default_authority: 'provider',
      toggle: { field: 'enable_thinking', wire_type: 'boolean', values: ['enabled', 'disabled'], provider_default: 'enabled' },
      ...(max ? { effort: { field: 'reasoning_effort', wire_type: 'string' as const, values: ['low', 'medium', 'xhigh'], aliases: { high: 'xhigh' }, provider_default: 'xhigh' } } : {}),
      budget: { field: 'thinking_budget', minimum: max ? 0 : 1, maximum: 262144, provider_default: max ? 131072 : 262144,
        requires_active_reasoning: true, mutually_exclusive_with_effort: max },
      thinking_clear_supported: false, toggle_and_effort_mutually_exclusive: false,
    },
  },
} as ModelExecutionProfile);

describe('exact model parameter description', () => {
  it('distinguishes output default, thinking budget and native levels without stale model data', () => {
    const { rerender } = render(<ModelParameterDescription profile={profile(true)} />);
    expect(screen.getByText(/8,192/)).toBeInTheDocument();
    expect(screen.getByText(/0–262,144/)).toBeInTheDocument();
    expect(screen.getByText(/high → xhigh/)).toBeInTheDocument();
    expect(screen.getByText(/不可与思考强度同时设置/)).toBeInTheDocument();
    rerender(<ModelParameterDescription profile={profile(false)} />);
    expect(screen.queryByText(/high → xhigh/)).not.toBeInTheDocument();
    expect(screen.getByText(/1–262,144/)).toBeInTheDocument();
    expect(screen.getByText(/未声明分档强度/)).toBeInTheDocument();
  });
  it('does not turn missing budget or old-server metadata into zero or a guessed control', () => {
    const item = profile(false);
    delete item.parameter_controls!.reasoning!.budget;
    const { rerender } = render(<ModelParameterDescription profile={item} />);
    expect(screen.getByText(/未声明可配置预算/)).toBeInTheDocument();
    delete item.parameter_controls;
    rerender(<ModelParameterDescription profile={item} />);
    expect(screen.queryByRole('region', { name: '所选模型的参数说明' })).not.toBeInTheDocument();
  });
});
