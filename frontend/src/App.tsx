import { translate as t, translateLabel, useI18n, LanguageSwitcher } from './i18n';
import { Suspense, useState, useCallback, useEffect } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Nav, Button, Input, Tag, Typography, Avatar, Spin, Toast } from '@douyinfe/semi-ui';
import {
  IconExit,
  IconBolt,
  IconSearch,
  IconMenu,
  IconClose,
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
  licenseRestrictionMessage,
  licenseFeatureLabel,
} from './navigation';
import { roleLabel } from './utils/displayLabels';
import { EmptyState, PageHeader, Surface } from './ui';

const { Sider, Content } = Layout;
const { Text } = Typography;

export default function App() {
  useI18n();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [globalSearch, setGlobalSearch] = useState('');
  const [loggingOut, setLoggingOut] = useState(false);
  const toggleSidebar = useCallback(() => setSidebarOpen(prev => !prev), []);
  const closeSidebar = useCallback(() => setSidebarOpen(false), []);
  useEffect(() => {
    if (!sidebarOpen) return;
    const dismiss = (event: KeyboardEvent) => {
      if (event.key === 'Escape') closeSidebar();
    };
    document.addEventListener('keydown', dismiss);
    return () => document.removeEventListener('keydown', dismiss);
  }, [sidebarOpen, closeSidebar]);

  const navigate = useNavigate();
  const location = useLocation();
  const logout = useAuthStore((s) => s.logout);
  const user = useAuthStore((s) => s.user);
  const authz = authzContextForUser(user);
  const licenseAccess = user?.license_access;
  const routeWithSearch = `${location.pathname}${location.search}`;
  const navItems = getNavItemsForAuthz(authz, licenseAccess).map(item => ({ ...item, text: translateLabel(item.text), items: item.items?.map(child => ({ ...child, text: translateLabel(child.text) })) }));
  const openKeys = navItems.filter(item => item.items?.length).map(item => item.itemKey);
  const currentRoleLabel = roleLabel(authz.organizationRole, t("未加载组织角色"));
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
      Toast.error(t("退出失败，服务端尚未确认会话撤销，请稍后重试。"));
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
        aria-label={t("打开导航")}
        aria-expanded={sidebarOpen}
      />

      <div className={`sidebar-overlay ${sidebarOpen ? "open" : ""}`} onClick={closeSidebar} />

      <Layout className="ur-app-shell">
      <Sider className={`ur-sider ${sidebarOpen ? 'open' : ''}`}>
        {sidebarOpen && <Button className="ur-mobile-nav-close" icon={<IconClose />} theme="borderless" size="small" aria-label={t('关闭导航')} onClick={closeSidebar} />}
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
        <div className="ur-sider-footer"><div className="ur-mobile-language"><LanguageSwitcher /></div>
          <div
            className="ur-product-version"
            aria-label={t('UniRouter 版本 V{version}', { version: __UNIROUTER_VERSION__ })}
            title={t("产品版本；部署制品另有精确运行时版本与提交标识")}
          >
            V{__UNIROUTER_VERSION__}
          </div>
          <Button
            className="mobile-account-button"
            icon={<IconUser />}
            theme="borderless"
            block
            aria-label={t("打开移动端我的账号")}
            onClick={() => { closeSidebar(); navigate('/settings'); }}
          >
            {t("我的账号")}</Button>
          <Button
            icon={<IconExit />}
            theme="borderless"
            block
            loading={loggingOut}
            disabled={loggingOut}
            onClick={handleLogout}
          >
            {t("退出登录")}</Button>
        </div>
      </Sider>
      <Content className="ur-content">
        <header
          className={`ur-topbar${canSearchLogs ? '' : ' ur-topbar-without-search'}`}
          role="toolbar"
          aria-label={t("全局工具栏")}
        >
          {canSearchLogs && (
            <Input
              className="ur-global-search"
              aria-label={t("全局搜索执行请求")}
              prefix={<IconSearch />}
              value={globalSearch}
              onChange={setGlobalSearch}
              onEnterPress={submitGlobalSearch}
              onKeyDown={(event) => {
                if (event.key === 'Enter') submitGlobalSearch();
              }}
              placeholder={t("搜索请求日志：请求、模型、供应商、错误或证据标识")}
              showClear
            />
          )}
          <div className="ur-topbar-actions"><LanguageSwitcher />
            <Tag className="ur-env-pill" color={envBadge.color as never}>{envBadge.label}</Tag>
            {canOpenNotifications && (
              <Button
                icon={<IconBell />}
                theme="borderless"
                className="ur-icon-button"
                aria-label={notificationsLicensed ? t("通知") : t("通知（商业授权不可用）")}
                title={notificationsLicensed ? t("通知") : notificationsRestrictionMessage || t("当前商业授权不可用，通知中心暂不可用。")}
                disabled={!notificationsLicensed}
                onClick={() => navigate('/notifications')}
              />
            )}
            <button
              type="button"
              className="ur-user-chip"
              aria-label={t("打开我的账号")}
              onClick={() => navigate('/settings')}
              style={{ cursor: 'pointer', font: 'inherit', color: 'inherit', textAlign: 'left' }}
            >
              <Avatar size="small" color="purple">{(user?.username || 'U').slice(0, 1).toUpperCase()}</Avatar>
              <div className="ur-user-copy">
                <Text className="ur-user-name">{user?.username || 'Operator'}</Text>
                <Text className="ur-user-role">{translateLabel(currentRoleLabel)}</Text>
              </div>
            </button>
          </div>
        </header>
        {licenseRestricted && requiredLicenseFeature && licenseAccess ? (
          <div className="ur-page">
            <PageHeader
              eyebrow={t("商业授权")}
              title={t('{feature}暂不可用', { feature: translateLabel(licenseFeatureLabel(requiredLicenseFeature)) })}
              description={t("你的角色具备该功能权限，但当前部署的签名商业授权不允许使用它。")}
            />
            <Surface>
              <EmptyState
                title={t("当前授权不可使用此功能")}
                description={restrictionMessage}
                actions={isPlatformAdminRole(authz.organizationRole) ? (
                  <Button type="primary" onClick={() => navigate('/license')}>{t("查看或更新商业授权")}</Button>
                ) : undefined}
                minHeight={320}
              />
            </Surface>
          </div>
        ) : (
          <Suspense fallback={<div className="ur-route-loading" role="status" aria-label={t("正在加载页面")}><Spin size="large" /><span>{t("正在加载页面…")}</span></div>}>
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
    return { label: t("生产环境"), color: 'green' };
  case 'staging':
  case 'stage':
    return { label: t("预发环境"), color: 'blue' };
  case 'canary':
    return { label: t("灰度环境"), color: 'orange' };
  case 'dev':
  case 'development':
    return { label: t("开发环境"), color: 'cyan' };
  case 'local':
    return { label: t("本地环境"), color: 'grey' };
  case '':
    return { label: t("环境未配置"), color: 'grey' };
  default:
    return { label: raw, color: 'grey' };
  }
}
