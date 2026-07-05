import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import ApiDocsPage from '../ApiDocsPage';

const mockGetErrorDocs = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getErrorDocs: (...args: any[]) => mockGetErrorDocs(...args),
  },
}));

describe('ApiDocsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetErrorDocs.mockResolvedValue({
      data: [{
        code: 'auth_error.invalid_api_key',
        http_status: 401,
        summary: 'Invalid API key',
        resolution: 'Create a valid project API key.',
      }],
    });
  });

  it('renders the current API contract index and post-PRD1 routes', async () => {
    render(<ApiDocsPage />);

    await waitFor(() => {
      expect(mockGetErrorDocs).toHaveBeenCalled();
    });

    expect(screen.getByText('API 文档')).toBeTruthy();
    expect(screen.getByText(/当前 API 合同索引/)).toBeTruthy();
    expect(screen.queryByText(/PRD1 API index/)).toBeNull();
    expect(screen.queryByText(/PRD2 当前可用接口索引/)).toBeNull();

    [
      '/api/v1/auth/oidc/config',
      '/api/v1/usage/analytics',
      '/api/v1/internal-status',
      '/api/v1/error-docs',
      '/api/v1/model-registry',
      '/api/v1/notification-webhooks',
      '/api/v1/license',
      '/v1/embeddings',
    ].forEach((path) => {
      expect(screen.getAllByText(path).length).toBeGreaterThan(0);
    });

    expect(await screen.findByText('auth_error.invalid_api_key')).toBeTruthy();
  });
});

