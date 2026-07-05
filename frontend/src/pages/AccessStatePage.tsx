import { Button, Typography } from '@douyinfe/semi-ui';
import { IconBookOpenStroked } from '@douyinfe/semi-icons';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, Surface } from '../ui';
import { roleLabel } from '../utils/displayLabels';

const { Text } = Typography;

export default function AccessStatePage() {
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);
  const role = user?.role || '';
  const currentRoleLabel = roleLabel(role, '未识别角色');

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="访问控制"
        title="访问状态"
        description="当前账号没有可进入的管理首页。请确认角色、组织或项目成员关系后再继续。"
      />

      <Surface>
        <EmptyState
          title="暂无可用工作台"
          description="API 文档可作为参考资料，但不是当前账号的默认工作台。"
          actions={<Button icon={<IconBookOpenStroked />} onClick={() => navigate('/docs')}>查看 API 文档</Button>}
          minHeight={260}
        />
        <div style={{ textAlign: 'center', marginTop: -28 }}>
          <Text type="tertiary">当前角色：{currentRoleLabel}</Text>
        </div>
      </Surface>
    </div>
  );
}
