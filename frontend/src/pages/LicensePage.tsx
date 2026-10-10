import { PageHeader, Surface } from '../ui';

// Preserve old bookmarks without importing, displaying or modifying stored documents.
export default function LicensePage() {
  return (
    <div className="ur-page">
      <PageHeader title="开源许可" description="UniRouter 使用 Apache License 2.0，无需商业 License 即可使用核心功能。" />
      <Surface>
        <p>历史商业授权入口已退役。账号权限、项目策略、预算和模型发布校验仍然生效。</p>
        <p>软件许可条款请参阅仓库中的 LICENSE；第三方组件保留各自许可声明。</p>
      </Surface>
    </div>
  );
}
