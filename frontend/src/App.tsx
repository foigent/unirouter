import { useState, useCallback } from 'react';
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
  {
    itemKey: 'g-overview',
    text: '概览',
    icon: <IconGridRectangle />,
    items: [
      { itemKey: '/', text: '仪表盘', icon: <IconGridRectangle /> },
    ],
  },
  {
    itemKey: 'g-traffic',
    text: 'AI 流量',
    icon: <IconServer />,
    items: [
      { itemKey: '/channels', text: '渠道管理', icon: <IconServer /> },
      { itemKey: '/channel-keys', text: '上游密钥', icon: <IconKey /> },
      { itemKey: '/keys', text: 'API 密钥', icon: <IconKey /> },
      { itemKey: '/logs', text: '调用日志', icon: <IconFile /> },
      { itemKey: '/test', text: '渠道测试', icon: <IconBolt /> },
    ],
  },
  {
    itemKey: 'g-billing',
    text: '账单',
    icon: <IconCoinMoney />,
    items: [
      { itemKey: '/wallet', text: '钱包管理', icon: <IconCoinMoney /> },
      { itemKey: '/model-prices', text: '模型价格', icon: <IconPriceTag /> },
      { itemKey: '/org-model-prices', text: '组织倍率', icon: <IconPriceTag /> },
      { itemKey: '/reconciliation', text: 'Token对账', icon: <IconSearch /> },
      { itemKey: '/grants', text: '赠送管理', icon: <IconGift /> },
    ],
  },
  {
    itemKey: 'g-access',
    text: '权限',
    icon: <IconUser />,
    items: [
      { itemKey: '/users', text: '用户管理', icon: <IconUser /> },
      { itemKey: '/audit-logs', text: '审计日志', icon: <IconSearch /> },
    ],
  },
  {
    itemKey: 'g-system',
    text: '系统',
    icon: <IconSetting />,
    items: [
      { itemKey: '/settings', text: '系统设置', icon: <IconSetting /> },
    ],
  },
];

const defaultOpenKeys = ['g-overview', 'g-traffic', 'g-billing', 'g-access', 'g-system'];

export default function App() {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const toggleSidebar = useCallback(() => setSidebarOpen(prev => !prev), []);
  const closeSidebar = useCallback(() => setSidebarOpen(false), []);

  const navigate = useNavigate();
  const location = useLocation();
  const logout = useAuthStore((s) => s.logout);

  const handleNavClick = (data: { itemKey?: string | number; domEvent?: MouseEvent; isOpen?: boolean }) => {
    const key = data.itemKey;
    if (typeof key === 'string' && key.startsWith('/')) {
      navigate(key);
    }
  };

  return (
    <>
      {/* Mobile menu toggle */}
      <div className="mobile-menu-toggle" onClick={toggleSidebar}>
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M3 12h18M3 6h18M3 18h18" />
        </svg>
      </div>
      
      {/* Sidebar overlay */}
      <div className={`sidebar-overlay ${sidebarOpen ? "open" : ""}`} onClick={closeSidebar} />
      
      <Layout style={{ height: '100vh' }}>
      <Sider className={sidebarOpen ? 'open' : ''} style={{ borderRight: '1px solid var(--semi-color-border)' }}>
        <div style={{ padding: '24px 24px 16px', display: 'flex', alignItems: 'center', gap: 10 }}>
          <IconBolt size="extra-large" style={{ color: 'var(--semi-color-primary)' }} />
          <h1 style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>UniRoute</h1>
        </div>
        <Nav
          selectedKeys={[location.pathname]}
          defaultOpenKeys={defaultOpenKeys}
          items={navItems}
          onClick={handleNavClick}
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
    </>
  );
}
