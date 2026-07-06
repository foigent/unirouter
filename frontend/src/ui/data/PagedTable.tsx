import type { ReactNode } from 'react';
import { Table } from '@douyinfe/semi-ui';
import EmptyState from '../feedback/EmptyState';

interface PagedTableProps<T extends Record<string, any> = Record<string, any>> {
  columns: any[];
  dataSource: T[];
  loading?: boolean;
  rowKey?: string | ((record: T) => string);
  page: number;
  pageSize: number;
  total: number;
  onPageChange: (page: number) => void;
  onPageSizeChange?: (pageSize: number) => void;
  pageSizeOpts?: number[];
  showSizeChanger?: boolean;
  empty?: ReactNode;
}

export default function PagedTable<T extends Record<string, any> = Record<string, any>>({
  columns,
  dataSource,
  loading = false,
  rowKey = 'id',
  page,
  pageSize,
  total,
  onPageChange,
  onPageSizeChange,
  pageSizeOpts,
  showSizeChanger = false,
  empty,
}: PagedTableProps<T>) {
  return (
    <div className="ur-table-shell">
      <Table
        columns={columns}
        dataSource={dataSource}
        loading={loading}
        rowKey={rowKey as never}
        empty={empty ?? <EmptyState title="暂无数据" minHeight={180} />}
        pagination={{
          currentPage: page,
          pageSize,
          total,
          pageSizeOpts,
          showSizeChanger,
          onPageChange,
          onPageSizeChange,
        }}
      />
    </div>
  );
}
