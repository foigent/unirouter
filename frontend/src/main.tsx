import React, { lazy, Suspense } from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { Button, Spin } from '@douyinfe/semi-ui'
import './index.css'
import ErrorBoundary from './components/ErrorBoundary'
import { profileNeedsRefresh, useAuthStore } from './store/auth'
import { authzContextForUser, getDefaultPathForAuthz, isRouteAllowedForAuthz } from './navigation'

const App = lazy(() => import('./App'))
const LoginPage = lazy(() => import('./pages/LoginPage'))
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const DeveloperConsolePage = lazy(() => import('./pages/DeveloperConsolePage'))
const ChannelsPage = lazy(() => import('./pages/ChannelsPage'))
const KeysPage = lazy(() => import('./pages/KeysPage'))
const WorkloadApplicationsPage = lazy(() => import('./pages/WorkloadApplicationsPage'))
const WalletPage = lazy(() => import('./pages/WalletPage'))
const ChannelKeysPage = lazy(() => import('./pages/ChannelKeysPage'))
const LogsPage = lazy(() => import('./pages/LogsPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const LicensePage = lazy(() => import('./pages/LicensePage'))
const ModelGovernancePage = lazy(() => import('./pages/ModelGovernancePage'))
const ModelBillingPage = lazy(() => import('./pages/ModelBillingPage'))
const AuditLogsPage = lazy(() => import('./pages/AuditLogsPage'))
const ReconciliationPage = lazy(() => import('./pages/ReconciliationPage'))
const NotificationCenterPage = lazy(() => import('./pages/NotificationCenterPage'))
const UsageAnalyticsPage = lazy(() => import('./pages/UsageAnalyticsPage'))
const CostOptimizationPage = lazy(() => import('./pages/CostOptimizationPage'))
const InternalStatusPage = lazy(() => import('./pages/InternalStatusPage'))
const TestPage = lazy(() => import('./pages/TestPage'))
const ApiDocsPage = lazy(() => import('./pages/ApiDocsPage'))
const AccessStatePage = lazy(() => import('./pages/AccessStatePage'))
const DeploymentNetworkPage = lazy(() => import('./pages/DeploymentNetworkPage'))
const IntegrationsPage = lazy(() => import('./pages/IntegrationsPage'))
const LimitPoliciesPage = lazy(() => import('./pages/LimitPoliciesPage'))
const GuardrailPoliciesPage = lazy(() => import('./pages/GuardrailPoliciesPage'))
const GovernanceSimulationPage = lazy(() => import('./pages/GovernanceSimulationPage'))
const AgentTrustPage = lazy(() => import('./pages/AgentTrustPage'))
const AgentTrustEvidencePage = lazy(() => import('./pages/AgentTrustEvidencePage'))
const ProvenancePassportPage = lazy(() => import('./pages/ProvenancePassportPage'))
const RiskControlPage = lazy(() => import('./pages/RiskControlPage'))
const MediaJobsPage = lazy(() => import('./pages/MediaJobsPage'))
const OrganizationMembersPage = lazy(() => import('./pages/OrganizationMembersPage'))

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
  const user = useAuthStore((s) => s.user)
  const authz = authzContextForUser(user)
  const routeWithSearch = `${location.pathname}${location.search}`

  if (!isRouteAllowedForAuthz(routeWithSearch, authz)) {
    return <Navigate to={getDefaultPathForAuthz(authz)} replace />
  }
  return <>{children}</>
}

function HomeRoute() {
  const user = useAuthStore((s) => s.user)
  const defaultPath = getDefaultPathForAuthz(authzContextForUser(user))
  if (defaultPath !== '/') return <Navigate to={defaultPath} replace />
  return (
    <RoleRoute>
      <DashboardPage />
    </RoleRoute>
  )
}

function UnknownRoute() {
  const user = useAuthStore((s) => s.user)
  return <Navigate to={getDefaultPathForAuthz(authzContextForUser(user))} replace />
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <Suspense fallback={<RouteLoading />}>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<ProtectedRoute><App /></ProtectedRoute>}>
              <Route index element={<HomeRoute />} />
              <Route path="developer" element={<RoleRoute><DeveloperConsolePage /></RoleRoute>} />
              <Route path="channels" element={<RoleRoute><ChannelsPage /></RoleRoute>} />
              <Route path="governance-simulation" element={<RoleRoute><GovernanceSimulationPage /></RoleRoute>} />
              <Route path="keys" element={<RoleRoute><KeysPage /></RoleRoute>} />
              <Route path="applications" element={<RoleRoute><WorkloadApplicationsPage /></RoleRoute>} />
              <Route path="limit-policies" element={<RoleRoute><LimitPoliciesPage /></RoleRoute>} />
              <Route path="guardrail-policies" element={<RoleRoute><GuardrailPoliciesPage /></RoleRoute>} />
              <Route path="wallet" element={<RoleRoute><WalletPage /></RoleRoute>} />
              <Route path="channel-keys" element={<RoleRoute><ChannelKeysPage /></RoleRoute>} />
              <Route path="logs" element={<RoleRoute><LogsPage /></RoleRoute>} />
              <Route path="organization-members" element={<RoleRoute><OrganizationMembersPage /></RoleRoute>} />
              <Route path="users" element={<RoleRoute><Navigate to="/organization-members" replace /></RoleRoute>} />
              <Route path="project-management" element={<RoleRoute><Navigate to="/organization-members?section=projects" replace /></RoleRoute>} />
              <Route path="member-lifecycle" element={<RoleRoute><Navigate to="/organization-members?section=lifecycle" replace /></RoleRoute>} />
              <Route path="agent-trust" element={<RoleRoute><AgentTrustPage /></RoleRoute>} />
              <Route path="agent-trust-evidence" element={<RoleRoute><AgentTrustEvidencePage /></RoleRoute>} />
              <Route path="provenance-passport" element={<RoleRoute><ProvenancePassportPage /></RoleRoute>} />
              <Route path="risk-control" element={<RoleRoute><RiskControlPage /></RoleRoute>} />
              <Route path="media-jobs" element={<RoleRoute><MediaJobsPage /></RoleRoute>} />
              <Route path="model-governance" element={<RoleRoute><ModelGovernancePage /></RoleRoute>} />
              <Route path="model-prices" element={<RoleRoute><ModelBillingPage /></RoleRoute>} />
              <Route path="org-model-prices" element={<Navigate to="/model-prices?tab=org" replace />} />
              <Route path="grants" element={<Navigate to="/wallet?tab=grants" replace />} />
              <Route path="audit-logs" element={<RoleRoute><AuditLogsPage /></RoleRoute>} />
              <Route path="data-exchange" element={<RoleRoute><Navigate to="/integrations?tab=xinsight" replace /></RoleRoute>} />
              <Route path="deployment" element={<RoleRoute><DeploymentNetworkPage /></RoleRoute>} />
              <Route path="integrations" element={<RoleRoute><IntegrationsPage /></RoleRoute>} />
              <Route path="reconciliation" element={<RoleRoute><ReconciliationPage /></RoleRoute>} />
              <Route path="usage-analytics" element={<RoleRoute><UsageAnalyticsPage /></RoleRoute>} />
              <Route path="cost-optimization" element={<RoleRoute><CostOptimizationPage /></RoleRoute>} />
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
      </Suspense>
    </ErrorBoundary>
  </React.StrictMode>
)
