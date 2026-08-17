import { describe, expect, it } from 'vitest';
import { projectDisplayName, projectOption } from '../projectDisplay';

describe('project display', () => {
  it('localizes the system default without exposing its database id', () => {
    expect(projectDisplayName({ id: 37, name: 'Default Project', slug: 'default' })).toBe('默认项目');
    expect(projectOption({ id: 37, name: 'Default Project', slug: 'default' })).toEqual({ value: 37, label: '默认项目' });
  });

  it('preserves a customer project name', () => {
    expect(projectDisplayName({ id: 42, name: '智能客服', slug: 'support' })).toBe('智能客服');
    expect(projectOption({ id: 42, name: '智能客服', slug: 'support' })).toEqual({ value: 42, label: '智能客服 (support)' });
  });

  it('retains an exact technical fallback when no display metadata exists', () => {
    expect(projectDisplayName({ id: 51 })).toBe('项目 #51');
  });
});
