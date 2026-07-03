import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { Spin } from '@douyinfe/semi-ui'
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
import ModelPricesPage from './pages/ModelPricesPage'
import OrgModelPricesPage from './pages/OrgModelPricesPage'
import GrantsPage from './pages/GrantsPage'
import AuditLogsPage from './pages/AuditLogsPage'
import ReconciliationPage from './pages/ReconciliationPage'
import NotificationCenterPage from './pages/NotificationCenterPage'
import UsageAnalyticsPage from './pages/UsageAnalyticsPage'
import InternalStatusPage from './pages/InternalStatusPage'
import TestPage from './pages/TestPage'
import ApiDocsPage from './pages/ApiDocsPage'
import ErrorBoundary from './components/ErrorBoundary'
import { useAuthStore } from './store/auth'
import { getDefaultPathForRole, isProjectConsoleRole, isRouteAllowedForRole } from './navigation'

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
  const loadProfile = useAuthStore((s) => s.loadProfile)

  React.useEffect(() => {
    if (isAuthenticated && !user?.role && !profileLoading) {
      loadProfile().catch(() => {})
    }
  }, [isAuthenticated, loadProfile, profileLoading, user?.role])

  if (!isAuthenticated) return <Navigate to="/login" replace />
  if (!user?.role) return <RouteLoading />
  return <>{children}</>
}

function RoleRoute({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const role = useAuthStore((s) => s.user?.role)

  if (!isRouteAllowedForRole(location.pathname, role)) {
    return <Navigate to={getDefaultPathForRole(role)} replace />
  }
  return <>{children}</>
}

function HomeRoute() {
  const role = useAuthStore((s) => s.user?.role)
  if (isProjectConsoleRole(role)) return <Navigate to="/developer" replace />
  return (
    <RoleRoute>
      <DashboardPage />
    </RoleRoute>
  )
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
            <Route path="keys" element={<RoleRoute><KeysPage /></RoleRoute>} />
            <Route path="wallet" element={<RoleRoute><WalletPage /></RoleRoute>} />
            <Route path="channel-keys" element={<RoleRoute><ChannelKeysPage /></RoleRoute>} />
            <Route path="logs" element={<RoleRoute><LogsPage /></RoleRoute>} />
            <Route path="users" element={<RoleRoute><UsersPage /></RoleRoute>} />
            <Route path="model-governance" element={<RoleRoute><ModelGovernancePage /></RoleRoute>} />
            <Route path="model-prices" element={<RoleRoute><ModelPricesPage /></RoleRoute>} />
            <Route path="org-model-prices" element={<RoleRoute><OrgModelPricesPage /></RoleRoute>} />
            <Route path="grants" element={<RoleRoute><GrantsPage /></RoleRoute>} />
            <Route path="audit-logs" element={<RoleRoute><AuditLogsPage /></RoleRoute>} />
            <Route path="reconciliation" element={<RoleRoute><ReconciliationPage /></RoleRoute>} />
            <Route path="usage-analytics" element={<RoleRoute><UsageAnalyticsPage /></RoleRoute>} />
            <Route path="notifications" element={<RoleRoute><NotificationCenterPage /></RoleRoute>} />
            <Route path="status" element={<RoleRoute><InternalStatusPage /></RoleRoute>} />
            <Route path="test" element={<RoleRoute><TestPage /></RoleRoute>} />
            <Route path="settings" element={<RoleRoute><SettingsPage /></RoleRoute>} />
            <Route path="license" element={<RoleRoute><LicensePage /></RoleRoute>} />
            <Route path="docs" element={<ApiDocsPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>
)
