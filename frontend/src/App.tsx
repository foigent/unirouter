import { Suspense, useState, useCallback } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Nav, Button, Input, Tag, Typography, Avatar, Spin, Toast } from '@douyinfe/semi-ui';
import {
  IconExit,
  IconBolt,
  IconSearch,
  IconMenu,
  IconBell,
  IconUser,
} from '@douyinfe/semi-icons';
import { useAuthStore } from './store/auth';
import {
  authzContextForUser,
  canOpenUnscopedNotifications,
  canSearchRequestLogs,
  getNavItemsForAuthz,
  getRequiredLicenseFeatureForRoute,
  hasLicensedFeature,
  isPlatformAdminRole,
  isRouteAllowedForAuthz,
  isRouteLicensed,
  licenseFeatureLabel,
  licenseRestrictionMessage,
} from './navigation';
import { roleLabel } from './utils/displayLabels';
import { EmptyState, PageHeader, Surface } from './ui';

const { Sider, Content } = Layout;
const { Text } = Typography;

export default function App() {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [globalSearch, setGlobalSearch] = useState('');
  const [loggingOut, setLoggingOut] = useState(false);
  const toggleSidebar = useCallback(() => setSidebarOpen(prev => !prev), []);
  const closeSidebar = useCallback(() => setSidebarOpen(false), []);

  const navigate = useNavigate();
  const location = useLocation();
  const logout = useAuthStore((s) => s.logout);
  const user = useAuthStore((s) => s.user);
  const authz = authzContextForUser(user);
  const licenseAccess = user?.license_access;
  const routeWithSearch = `${location.pathname}${location.search}`;
  const navItems = getNavItemsForAuthz(authz, licenseAccess);
  const openKeys = navItems.filter(item => item.items?.length).map(item => item.itemKey);
  const currentRoleLabel = roleLabel(authz.organizationRole, '未加载组织角色');
  const canOpenNotifications = canOpenUnscopedNotifications(authz);
  const notificationsLicensed = hasLicensedFeature(licenseAccess, 'notifications');
  const notificationsRestrictionMessage = licenseAccess
    ? licenseRestrictionMessage(licenseAccess, 'notifications')
    : '';
  const canSearchLogs = canSearchRequestLogs(authz);
  const envBadge = getEnvironmentBadge();
  const requiredLicenseFeature = getRequiredLicenseFeatureForRoute(routeWithSearch);
  const licenseRestricted = requiredLicenseFeature !== undefined &&
    isRouteAllowedForAuthz(routeWithSearch, authz) &&
    !isRouteLicensed(routeWithSearch, licenseAccess);
  const restrictionMessage = requiredLicenseFeature && licenseAccess
    ? licenseRestrictionMessage(licenseAccess, requiredLicenseFeature)
    : '';

  const handleNavClick = (data: { itemKey?: string | number; domEvent?: MouseEvent; isOpen?: boolean }) => {
    const key = data.itemKey;
    if (typeof key === 'string' && key.startsWith('/')) {
      navigate(key);
      closeSidebar();
    }
  };

  const submitGlobalSearch = () => {
    if (!canSearchLogs) return;
    const query = globalSearch.trim();
    if (!query) {
      navigate('/logs');
      return;
    }
    navigate(`/logs?search=${encodeURIComponent(query)}`);
  };

  const handleLogout = async () => {
    if (loggingOut) return;
    setLoggingOut(true);
    try {
      await logout();
      navigate('/login');
    } catch {
      Toast.error('退出失败，服务端尚未确认会话撤销，请稍后重试。');
    } finally {
      setLoggingOut(false);
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
          openKeys={openKeys}
          items={navItems}
          onClick={handleNavClick}
          style={{ border: 'none' }}
        />
        <div className="ur-sider-footer">
          <Button
            className="mobile-account-button"
            icon={<IconUser />}
            theme="borderless"
            block
            aria-label="打开移动端我的账号"
            onClick={() => { closeSidebar(); navigate('/settings'); }}
          >
            我的账号
          </Button>
          <Button
            icon={<IconExit />}
            theme="borderless"
            block
            loading={loggingOut}
            disabled={loggingOut}
            onClick={handleLogout}
          >
            退出登录
          </Button>
        </div>
      </Sider>
      <Content className="ur-content">
        <header
          className={`ur-topbar${canSearchLogs ? '' : ' ur-topbar-without-search'}`}
          role="toolbar"
          aria-label="全局工具栏"
        >
          {canSearchLogs && (
            <Input
              className="ur-global-search"
              prefix={<IconSearch />}
              value={globalSearch}
              onChange={setGlobalSearch}
              onEnterPress={submitGlobalSearch}
              onKeyDown={(event) => {
                if (event.key === 'Enter') submitGlobalSearch();
              }}
              placeholder="搜索请求日志：请求、模型、供应商、错误或证据标识"
              showClear
            />
          )}
          <div className="ur-topbar-actions">
            <Tag className="ur-env-pill" color={envBadge.color as never}>{envBadge.label}</Tag>
            {canOpenNotifications && (
              <Button
                icon={<IconBell />}
                theme="borderless"
                className="ur-icon-button"
                aria-label={notificationsLicensed ? '通知' : '通知（授权未包含）'}
                title={notificationsLicensed ? '通知' : notificationsRestrictionMessage || '当前商业授权未包含通知中心。'}
                disabled={!notificationsLicensed}
                onClick={() => navigate('/notifications')}
              />
            )}
            <button
              type="button"
              className="ur-user-chip"
              aria-label="打开我的账号"
              onClick={() => navigate('/settings')}
              style={{ cursor: 'pointer', font: 'inherit', color: 'inherit', textAlign: 'left' }}
            >
              <Avatar size="small" color="purple">{(user?.username || 'U').slice(0, 1).toUpperCase()}</Avatar>
              <div className="ur-user-copy">
                <Text className="ur-user-name">{user?.username || 'Operator'}</Text>
                <Text className="ur-user-role">{currentRoleLabel}</Text>
              </div>
            </button>
          </div>
        </header>
        {licenseRestricted && requiredLicenseFeature && licenseAccess ? (
          <div className="ur-page">
            <PageHeader
              eyebrow="商业授权"
              title={`${licenseFeatureLabel(requiredLicenseFeature)}暂不可用`}
              description="你的角色具备该功能权限，但当前部署的签名商业授权不允许使用它。"
            />
            <Surface>
              <EmptyState
                title="当前授权不可使用此功能"
                description={restrictionMessage}
                actions={isPlatformAdminRole(authz.organizationRole) ? (
                  <Button type="primary" onClick={() => navigate('/license')}>查看或更新商业授权</Button>
                ) : undefined}
                minHeight={320}
              />
            </Surface>
          </div>
        ) : (
          <Suspense fallback={<div className="ur-route-loading" role="status" aria-label="正在加载页面"><Spin size="large" /></div>}>
            <Outlet />
          </Suspense>
        )}
      </Content>
    </Layout>
    </>
  );
}

function getEnvironmentBadge() {
  const raw = String(import.meta.env.VITE_APP_ENV || import.meta.env.VITE_DEPLOY_ENV || '').trim().toLowerCase();
  switch (raw) {
  case 'prod':
  case 'production':
    return { label: '生产环境', color: 'green' };
  case 'staging':
  case 'stage':
    return { label: '预发环境', color: 'blue' };
  case 'canary':
    return { label: '灰度环境', color: 'orange' };
  case 'dev':
  case 'development':
    return { label: '开发环境', color: 'cyan' };
  case 'local':
    return { label: '本地环境', color: 'grey' };
  case '':
    return { label: '环境未配置', color: 'grey' };
  default:
    return { label: raw, color: 'grey' };
  }
}
