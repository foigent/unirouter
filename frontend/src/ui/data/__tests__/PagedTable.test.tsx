import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import PagedTable from '../PagedTable';

vi.mock('@douyinfe/semi-ui', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@douyinfe/semi-ui')>();

  return {
    ...actual,
    Table: ({ dataSource, empty, loading, pagination, rowKey }: any) => (
      <div
        data-testid="semi-table"
        data-loading={String(loading)}
        data-row-key={typeof rowKey === 'string' ? rowKey : 'fn'}
      >
        <span>{`page-${pagination.currentPage}-size-${pagination.pageSize}-total-${pagination.total}`}</span>
        <button onClick={() => pagination.onPageChange(3)}>page</button>
        <button onClick={() => pagination.onPageSizeChange?.(50)}>size</button>
        {dataSource.length === 0 ? empty : null}
      </div>
    ),
  };
});

describe('PagedTable', () => {
  it('passes table shell, empty state, and pagination callbacks through', () => {
    const onPageChange = vi.fn();
    const onPageSizeChange = vi.fn();

    render(
      <PagedTable
        columns={[{ title: 'Name', dataIndex: 'name' }]}
        dataSource={[]}
        loading
        page={2}
        pageSize={20}
        total={35}
        onPageChange={onPageChange}
        onPageSizeChange={onPageSizeChange}
      />,
    );

    expect(screen.getByTestId('semi-table')).toHaveAttribute('data-loading', 'true');
    expect(screen.getByTestId('semi-table')).toHaveAttribute('data-row-key', 'id');
    expect(screen.getByText('page-2-size-20-total-35')).toBeInTheDocument();
    expect(screen.getByText('暂无数据')).toBeInTheDocument();

    fireEvent.click(screen.getByText('page'));
    fireEvent.click(screen.getByText('size'));

    expect(onPageChange).toHaveBeenCalledWith(3);
    expect(onPageSizeChange).toHaveBeenCalledWith(50);
  });
});
