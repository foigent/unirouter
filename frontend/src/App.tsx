import { useState, useCallback } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Nav, Button, Input, Tag, Typography, Avatar } from '@douyinfe/semi-ui';
import {
  IconExit,
  IconBolt,
  IconSearch,
  IconMenu,
  IconBell,
} from '@douyinfe/semi-icons';
import { useAuthStore } from './store/auth';
import { getDefaultOpenKeysForRole, getNavItemsForRole, isRouteAllowedForRole, ROLE_LABELS } from './navigation';

const { Sider, Content } = Layout;
const { Text } = Typography;

export default function App() {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const toggleSidebar = useCallback(() => setSidebarOpen(prev => !prev), []);
  const closeSidebar = useCallback(() => setSidebarOpen(false), []);

  const navigate = useNavigate();
  const location = useLocation();
  const logout = useAuthStore((s) => s.logout);
  const user = useAuthStore((s) => s.user);
  const role = user?.role;
  const navItems = getNavItemsForRole(role);
  const defaultOpenKeys = getDefaultOpenKeysForRole(role);
  const roleLabel = ROLE_LABELS[role || ''] || role || '未加载角色';
  const canOpenNotifications = isRouteAllowedForRole('/notifications', role);

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
            <h1 className="ur-brand-name">UniRouter</h1>
            <div className="ur-brand-caption">Enterprise Intelligence Foundation</div>
          </div>
        </div>
        <Nav
          selectedKeys={[location.pathname]}
          defaultOpenKeys={defaultOpenKeys}
          items={navItems}
          onClick={handleNavClick}
          style={{ border: 'none' }}
        />
        <div className="ur-sider-footer">
          <Button
            icon={<IconExit />}
            theme="borderless"
            block
            onClick={() => { logout(); navigate('/login'); }}
          >
            退出登录
          </Button>
        </div>
      </Sider>
      <Content className="ur-content">
        <div className="ur-topbar">
          <Input
            className="ur-global-search"
            prefix={<IconSearch />}
            placeholder="搜索 Request ID、模型、密钥"
            showClear
          />
          <div className="ur-topbar-actions">
            <Tag className="ur-env-pill" color="green">生产环境</Tag>
            {canOpenNotifications && (
              <Button icon={<IconBell />} theme="borderless" className="ur-icon-button" aria-label="通知" onClick={() => navigate('/notifications')} />
            )}
            <div className="ur-user-chip">
              <Avatar size="small" color="purple">{(user?.username || 'U').slice(0, 1).toUpperCase()}</Avatar>
              <div className="ur-user-copy">
                <Text className="ur-user-name">{user?.username || 'Operator'}</Text>
                <Text className="ur-user-role">{roleLabel}</Text>
              </div>
            </div>
          </div>
        </div>
        <Outlet />
      </Content>
    </Layout>
    </>
  );
}
