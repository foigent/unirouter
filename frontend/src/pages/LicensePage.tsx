import { PageHeader, Surface } from '../ui';
import { useI18n } from '../i18n';

// Preserve old bookmarks without importing, displaying or modifying stored documents.
export default function LicensePage() {
  const { t } = useI18n();
  return (
    <div className="ur-page">
      <PageHeader title={t('开源许可')} description={t('UniRouter 使用 Apache License 2.0，无需商业 License 即可使用核心功能。')} />
      <Surface>
        <p>{t('历史商业授权入口已退役。账号权限、项目策略、预算和模型发布校验仍然生效。')}</p>
        <p>{t('软件许可条款请参阅仓库中的 LICENSE；第三方组件保留各自许可声明。')}</p>
      </Surface>
    </div>
  );
}
