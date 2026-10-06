import React, { lazy, Suspense } from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { LocaleProvider, Spin } from '@douyinfe/semi-ui'
import zh_CN from '@douyinfe/semi-ui/lib/es/locale/source/zh_CN'
import './index.css'
import { ROUTER_FUTURE_FLAGS } from './routing/routerFuture'
import ErrorBoundary from './components/ErrorBoundary'
import { useAuthStore } from './store/auth'
import ProtectedRoute from './routing/ProtectedRoute'
import { authzContextForUser, getDefaultPathForAuthz, isPlatformAdminRole, isRouteAllowedForAuthz } from './navigation'

const App = lazy(() => import('./App'))
const LoginPage = lazy(() => import('./pages/LoginPage'))
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const DeveloperConsolePage = lazy(() => import('./pages/DeveloperConsolePage'))
const ModelEndpointsPage = lazy(() => import('./pages/ModelEndpointsPage'))
const KeysPage = lazy(() => import('./pages/KeysPage'))
const ApplicationsV2Page = lazy(() => import('./pages/ApplicationsV2Page'))
const AccessReviewPage = lazy(() => import('./pages/AccessReviewPage'))
const ModelReadinessPage = lazy(() => import('./pages/ModelReadinessPage'))
const WalletPage = lazy(() => import('./pages/WalletPage'))
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
const ApiDocsPage = lazy(() => import('./pages/ApiDocsPage'))
const AccessStatePage = lazy(() => import('./pages/AccessStatePage'))
const DeploymentNetworkPage = lazy(() => import('./pages/DeploymentNetworkPage'))
const IntegrationsPage = lazy(() => import('./pages/IntegrationsPage'))
const XInsightCredentialRecoveryPage = lazy(() => import('./pages/XInsightCredentialRecoveryPage'))
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
    <div className="ur-route-loading" role="status" aria-label="正在加载页面">
      <Spin size="large" />
      <span>正在加载页面…</span>
    </div>
  )
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

function UnifiedModelAccessRoute({ view, children }: { view: 'catalog' | 'diagnostics'; children: React.ReactNode }) {
  const user = useAuthStore((s) => s.user)
  if (isPlatformAdminRole(user?.organization_role || user?.role)) {
    return <Navigate to={`/channels?tab=advanced&view=${view}`} replace />
  }
  return <RoleRoute>{children}</RoleRoute>
}

function UnknownRoute() {
  const user = useAuthStore((s) => s.user)
  return <Navigate to={getDefaultPathForAuthz(authzContextForUser(user))} replace />
}

function XInsightConnectorInstallRoute() {
  const location = useLocation()
  return <Navigate to={`/integrations?tab=xinsight${location.hash}`} replace />
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <Suspense fallback={<RouteLoading />}>
        <LocaleProvider locale={zh_CN}>
          <BrowserRouter future={ROUTER_FUTURE_FLAGS}>
            <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<ProtectedRoute><App /></ProtectedRoute>}>
              <Route index element={<HomeRoute />} />
              <Route path="developer" element={<RoleRoute><DeveloperConsolePage /></RoleRoute>} />
              <Route path="channels" element={<RoleRoute><ModelEndpointsPage /></RoleRoute>} />
              <Route path="governance-simulation" element={<RoleRoute><GovernanceSimulationPage /></RoleRoute>} />
              <Route path="keys" element={<RoleRoute><KeysPage /></RoleRoute>} />
				<Route path="applications" element={<RoleRoute><ApplicationsV2Page /></RoleRoute>} />
				<Route path="access-review" element={<RoleRoute><AccessReviewPage /></RoleRoute>} />
				<Route path="model-readiness" element={<UnifiedModelAccessRoute view="diagnostics"><ModelReadinessPage /></UnifiedModelAccessRoute>} />
              <Route path="limit-policies" element={<RoleRoute><LimitPoliciesPage /></RoleRoute>} />
              <Route path="guardrail-policies" element={<RoleRoute><GuardrailPoliciesPage /></RoleRoute>} />
              <Route path="wallet" element={<RoleRoute><WalletPage /></RoleRoute>} />
              <Route path="channel-keys" element={<RoleRoute><Navigate to="/channels?tab=credentials" replace /></RoleRoute>} />
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
              <Route path="model-governance" element={<UnifiedModelAccessRoute view="catalog"><ModelGovernancePage /></UnifiedModelAccessRoute>} />
              <Route path="model-prices" element={<RoleRoute><ModelBillingPage /></RoleRoute>} />
              <Route path="org-model-prices" element={<Navigate to="/model-prices?tab=org" replace />} />
              <Route path="grants" element={<Navigate to="/wallet?tab=grants" replace />} />
              <Route path="audit-logs" element={<RoleRoute><AuditLogsPage /></RoleRoute>} />
              <Route path="data-exchange" element={<RoleRoute><Navigate to="/integrations?tab=xinsight" replace /></RoleRoute>} />
              <Route path="deployment" element={<RoleRoute><DeploymentNetworkPage /></RoleRoute>} />
              <Route path="integrations" element={<RoleRoute><IntegrationsPage /></RoleRoute>} />
              <Route path="connectors/x-insight/install" element={<RoleRoute><XInsightConnectorInstallRoute /></RoleRoute>} />
              <Route path="connectors/x-insight/recover" element={<RoleRoute><XInsightCredentialRecoveryPage /></RoleRoute>} />
              <Route path="reconciliation" element={<RoleRoute><ReconciliationPage /></RoleRoute>} />
              <Route path="usage-analytics" element={<RoleRoute><UsageAnalyticsPage /></RoleRoute>} />
              <Route path="cost-optimization" element={<RoleRoute><CostOptimizationPage /></RoleRoute>} />
              <Route path="notifications" element={<RoleRoute><NotificationCenterPage /></RoleRoute>} />
              <Route path="status" element={<RoleRoute><InternalStatusPage /></RoleRoute>} />
              <Route path="test" element={<Navigate to="/channels" replace />} />
              <Route path="settings" element={<RoleRoute><SettingsPage /></RoleRoute>} />
              <Route path="license" element={<RoleRoute><LicensePage /></RoleRoute>} />
              <Route path="access" element={<RoleRoute><AccessStatePage /></RoleRoute>} />
              <Route path="docs" element={<ApiDocsPage />} />
              <Route path="*" element={<UnknownRoute />} />
            </Route>
            </Routes>
          </BrowserRouter>
        </LocaleProvider>
      </Suspense>
    </ErrorBoundary>
  </React.StrictMode>
)
