import type { ReactNode } from 'react';
import { Button, Table } from '@douyinfe/semi-ui';
import EmptyState from '../feedback/EmptyState';
import { useI18n } from '../../i18n';
import { SERVER_PAGE_SIZES } from '../../utils/pagination';

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
  loadError?: { title: string; retry: () => void };
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
  pageSizeOpts = SERVER_PAGE_SIZES,
  showSizeChanger = false,
  empty,
  loadError,
}: PagedTableProps<T>) {
  const { t } = useI18n();
  let resolvedEmpty = empty ?? <EmptyState title={t('暂无数据')} minHeight={180} />;
  if (loading && dataSource.length === 0) {
    resolvedEmpty = <EmptyState title={t('正在加载数据')} description={t('正在读取当前范围，请稍候。')} minHeight={180} />;
  } else if (loadError) {
    resolvedEmpty = (
      <EmptyState
        title={loadError.title}
        actions={<Button onClick={loadError.retry}>{t('重试')}</Button>}
        minHeight={180}
      />
    );
  }
  return (
    <div className="ur-table-shell">
      <Table
        columns={columns}
        dataSource={dataSource}
        loading={loading}
        rowKey={rowKey as never}
        empty={resolvedEmpty}
        pagination={{
          currentPage: page,
          pageSize,
          total,
          pageSizeOpts,
          showSizeChanger,
          preventPageChangeOnPageSizeChange: true,
          onPageChange,
          onPageSizeChange,
        }}
      />
    </div>
  );
}
