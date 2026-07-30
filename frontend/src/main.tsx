import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { Button, Spin } from '@douyinfe/semi-ui'
import './index.css'
import App from './App'
import LoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import DeveloperConsolePage from './pages/DeveloperConsolePage'
import ChannelsPage from './pages/ChannelsPage'
import KeysPage from './pages/KeysPage'
import WalletPage from './pages/WalletPage'
import ChannelKeysPage from './pages/ChannelKeysPage'
import LogsPage from './pages/LogsPage'
import SettingsPage from './pages/SettingsPage'
import LicensePage from './pages/LicensePage'
import UsersPage from './pages/UsersPage'
import ModelGovernancePage from './pages/ModelGovernancePage'
import ModelBillingPage from './pages/ModelBillingPage'
import AuditLogsPage from './pages/AuditLogsPage'
import ReconciliationPage from './pages/ReconciliationPage'
import NotificationCenterPage from './pages/NotificationCenterPage'
import UsageAnalyticsPage from './pages/UsageAnalyticsPage'
import InternalStatusPage from './pages/InternalStatusPage'
import TestPage from './pages/TestPage'
import ApiDocsPage from './pages/ApiDocsPage'
import AccessStatePage from './pages/AccessStatePage'
import ErrorBoundary from './components/ErrorBoundary'
import DataExchangePage from './pages/DataExchangePage'
import LimitPoliciesPage from './pages/LimitPoliciesPage'
import GuardrailPoliciesPage from './pages/GuardrailPoliciesPage'
import ProjectManagementPage from './pages/ProjectManagementPage'
import GovernanceSimulationPage from './pages/GovernanceSimulationPage'
import { profileNeedsRefresh, useAuthStore } from './store/auth'
import { getDefaultPathForRole, isRouteAllowedForRole } from './navigation'

function RouteLoading() {
  return (
    <div className="ur-route-loading">
      <Spin size="large" />
    </div>
  )
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const user = useAuthStore((s) => s.user)
  const profileLoading = useAuthStore((s) => s.profileLoading)
  const profileError = useAuthStore((s) => s.profileError)
  const loadProfile = useAuthStore((s) => s.loadProfile)
  const profileNeedsLoad = profileNeedsRefresh(user)

  React.useEffect(() => {
    if (isAuthenticated && profileNeedsLoad && !profileLoading && !profileError) {
      loadProfile().catch(() => {})
    }
  }, [isAuthenticated, loadProfile, profileError, profileLoading, profileNeedsLoad])

  if (!isAuthenticated) return <Navigate to="/login" replace />
  if (profileNeedsLoad && profileError) {
    return (
      <div className="ur-route-error" role="alert">
        <strong>暂时无法验证登录状态</strong>
        <span>{profileError}</span>
        <Button theme="solid" onClick={() => loadProfile().catch(() => {})}>重试</Button>
      </div>
    )
  }
  if (profileNeedsLoad) return <RouteLoading />
  return <>{children}</>
}

function RoleRoute({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const role = useAuthStore((s) => s.user?.role)
  const routeWithSearch = `${location.pathname}${location.search}`

  if (!isRouteAllowedForRole(routeWithSearch, role)) {
    return <Navigate to={getDefaultPathForRole(role)} replace />
  }
  return <>{children}</>
}

function HomeRoute() {
  const role = useAuthStore((s) => s.user?.role)
  const defaultPath = getDefaultPathForRole(role)
  if (defaultPath !== '/') return <Navigate to={defaultPath} replace />
  return (
    <RoleRoute>
      <DashboardPage />
    </RoleRoute>
  )
}

function UnknownRoute() {
  const role = useAuthStore((s) => s.user?.role)
  return <Navigate to={getDefaultPathForRole(role)} replace />
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<ProtectedRoute><App /></ProtectedRoute>}>
            <Route index element={<HomeRoute />} />
            <Route path="developer" element={<RoleRoute><DeveloperConsolePage /></RoleRoute>} />
            <Route path="channels" element={<RoleRoute><ChannelsPage /></RoleRoute>} />
            <Route path="governance-simulation" element={<RoleRoute><GovernanceSimulationPage /></RoleRoute>} />
            <Route path="keys" element={<RoleRoute><KeysPage /></RoleRoute>} />
            <Route path="limit-policies" element={<RoleRoute><LimitPoliciesPage /></RoleRoute>} />
            <Route path="guardrail-policies" element={<RoleRoute><GuardrailPoliciesPage /></RoleRoute>} />
            <Route path="wallet" element={<RoleRoute><WalletPage /></RoleRoute>} />
            <Route path="channel-keys" element={<RoleRoute><ChannelKeysPage /></RoleRoute>} />
            <Route path="logs" element={<RoleRoute><LogsPage /></RoleRoute>} />
            <Route path="users" element={<RoleRoute><UsersPage /></RoleRoute>} />
            <Route path="project-management" element={<RoleRoute><ProjectManagementPage /></RoleRoute>} />
            <Route path="model-governance" element={<RoleRoute><ModelGovernancePage /></RoleRoute>} />
            <Route path="model-prices" element={<RoleRoute><ModelBillingPage /></RoleRoute>} />
            <Route path="org-model-prices" element={<Navigate to="/model-prices?tab=org" replace />} />
            <Route path="grants" element={<Navigate to="/wallet?tab=grants" replace />} />
            <Route path="audit-logs" element={<RoleRoute><AuditLogsPage /></RoleRoute>} />
            <Route path="data-exchange" element={<RoleRoute><DataExchangePage /></RoleRoute>} />
            <Route path="reconciliation" element={<RoleRoute><ReconciliationPage /></RoleRoute>} />
            <Route path="usage-analytics" element={<RoleRoute><UsageAnalyticsPage /></RoleRoute>} />
            <Route path="notifications" element={<RoleRoute><NotificationCenterPage /></RoleRoute>} />
            <Route path="status" element={<RoleRoute><InternalStatusPage /></RoleRoute>} />
            <Route path="test" element={<RoleRoute><TestPage /></RoleRoute>} />
            <Route path="settings" element={<RoleRoute><SettingsPage /></RoleRoute>} />
            <Route path="license" element={<RoleRoute><LicensePage /></RoleRoute>} />
            <Route path="access" element={<RoleRoute><AccessStatePage /></RoleRoute>} />
            <Route path="docs" element={<ApiDocsPage />} />
            <Route path="*" element={<UnknownRoute />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>
)
