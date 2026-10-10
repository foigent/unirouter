import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import LicensePage from '../LicensePage';

const request = vi.fn();
vi.mock('../../api/client', () => ({ api: { get: request, put: request } }));

describe('retired commercial license page', () => {
  it('explains Apache licensing without an import action or network request', () => {
    render(<LicensePage />);
    expect(screen.getByText(/Apache License 2.0/)).toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /导入|保存/ })).not.toBeInTheDocument();
    expect(request).not.toHaveBeenCalled();
  });
});
