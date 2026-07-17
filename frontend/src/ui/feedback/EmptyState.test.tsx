import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import EmptyState from './EmptyState';

describe('EmptyState', () => {
  it('renders its description once', () => {
    render(<EmptyState title="暂无记录" description="请调整筛选条件后重试。" />);

    expect(screen.getAllByText('请调整筛选条件后重试。')).toHaveLength(1);
  });
});
