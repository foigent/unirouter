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
  IconMenu,
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
      { itemKey: '/docs', text: 'API 文档', icon: <IconFile /> },
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
      <Button
        className="mobile-menu-toggle"
        icon={<IconMenu />}
        onClick={toggleSidebar}
        aria-label="打开导航"
      />

      <div className={`sidebar-overlay ${sidebarOpen ? "open" : ""}`} onClick={closeSidebar} />

      <Layout className="ur-app-shell">
      <Sider className={`ur-sider ${sidebarOpen ? 'open' : ''}`}>
        <div className="ur-brand">
          <div className="ur-brand-mark">
            <IconBolt size="large" />
          </div>
          <div>
            <h1 className="ur-brand-name">UniRoute</h1>
            <div className="ur-brand-caption">AI Gateway Console</div>
          </div>
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
      <Content className="ur-content">
        <Outlet />
      </Content>
    </Layout>
    </>
  );
}
