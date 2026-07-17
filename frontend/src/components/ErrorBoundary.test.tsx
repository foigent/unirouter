import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ErrorBoundary from './ErrorBoundary';

function BrokenPage(): never {
  throw new Error('internal SQL and token details');
}

describe('ErrorBoundary', () => {
  afterEach(() => vi.restoreAllMocks());

  it('logs diagnostics without exposing the raw exception to the user', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);

    render(<ErrorBoundary><BrokenPage /></ErrorBoundary>);

    expect(screen.getByText('页面出错了')).toBeTruthy();
    expect(screen.getByText(/页面暂时无法继续显示/)).toBeTruthy();
    expect(screen.queryByText(/internal SQL and token details/)).toBeNull();
  });
});
