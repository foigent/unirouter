import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Nav, Button } from '@douyinfe/semi-ui';
import {
  IconSetting,
  IconKey,
  IconServer,
  IconCoinMoney,
  IconFile,
  IconGridRectangle,
  IconExit,
  IconBolt,
  IconUser,
  IconPriceTag,
  IconGift,
  IconSearch,
} from '@douyinfe/semi-icons';
import { useAuthStore } from './store/auth';

const { Sider, Content } = Layout;

const navItems = [
  { path: '/', label: '仪表盘', icon: <IconGridRectangle /> },
  { path: '/channels', label: '渠道管理', icon: <IconServer /> },
  { path: '/keys', label: '密钥管理', icon: <IconKey /> },
  { path: '/wallet', label: '钱包管理', icon: <IconCoinMoney /> },
  { path: '/channel-keys', label: '上游密钥', icon: <IconKey /> },
  { path: '/logs', label: '调用日志', icon: <IconFile /> },
  { path: '/users', label: '用户管理', icon: <IconUser /> },
  { path: '/model-prices', label: '模型价格', icon: <IconPriceTag /> },
  { path: '/org-model-prices', label: '组织倍率', icon: <IconPriceTag /> },
  { path: '/grants', label: '赠送管理', icon: <IconGift /> },
  { path: '/audit-logs', label: '审计日志', icon: <IconSearch /> },
  { path: '/reconciliation', label: 'Token对账', icon: <IconSearch /> },
  { path: '/test', label: '渠道测试', icon: <IconBolt /> },
  { path: '/settings', label: '系统设置', icon: <IconSetting /> },
];

export default function App() {
  const navigate = useNavigate();
  const location = useLocation();
  const logout = useAuthStore((s) => s.logout);

  return (
    <Layout style={{ height: '100vh' }}>
      <Sider style={{ borderRight: '1px solid var(--semi-color-border)' }}>
        <div style={{ padding: '24px 24px 16px', display: 'flex', alignItems: 'center', gap: 10 }}>
          <IconBolt size="extra-large" style={{ color: 'var(--semi-color-primary)' }} />
          <h1 style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>UniRoute</h1>
        </div>
        <Nav
          selectedKeys={[location.pathname]}
          items={navItems.map((item) => ({
            itemKey: item.path,
            text: item.label,
            icon: item.icon,
          }))}
          onClick={({ itemKey }) => navigate(itemKey as string)}
          style={{ border: 'none' }}
          footer={
            <div style={{ padding: '12px 24px' }}>
              <Button
                icon={<IconExit />}
                theme="borderless"
                block
                onClick={() => { logout(); navigate('/login'); }}
              >
                退出登录
              </Button>
            </div>
          }
        />
      </Sider>
      <Content style={{ padding: 24, overflow: 'auto', background: 'var(--semi-color-bg-0)' }}>
        <Outlet />
      </Content>
    </Layout>
  );
}
