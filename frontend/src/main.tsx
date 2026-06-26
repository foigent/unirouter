import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import './index.css'
import App from './App'
import LoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import ChannelsPage from './pages/ChannelsPage'
import KeysPage from './pages/KeysPage'
import WalletPage from './pages/WalletPage'
import ChannelKeysPage from './pages/ChannelKeysPage'
import LogsPage from './pages/LogsPage'
import SettingsPage from './pages/SettingsPage'
import UsersPage from './pages/UsersPage'
import ModelPricesPage from './pages/ModelPricesPage'
import OrgModelPricesPage from './pages/OrgModelPricesPage'
import GrantsPage from './pages/GrantsPage'
import AuditLogsPage from './pages/AuditLogsPage'
import ErrorBoundary from './components/ErrorBoundary'
import { useAuthStore } from './store/auth'

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  if (!isAuthenticated) return <Navigate to="/login" replace />
  return <>{children}</>
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<ProtectedRoute><App /></ProtectedRoute>}>
            <Route index element={<DashboardPage />} />
            <Route path="channels" element={<ChannelsPage />} />
            <Route path="keys" element={<KeysPage />} />
            <Route path="wallet" element={<WalletPage />} />
            <Route path="channel-keys" element={<ChannelKeysPage />} />
            <Route path="logs" element={<LogsPage />} />
            <Route path="users" element={<UsersPage />} />
            <Route path="model-prices" element={<ModelPricesPage />} />
            <Route path="org-model-prices" element={<OrgModelPricesPage />} />
            <Route path="grants" element={<GrantsPage />} />
            <Route path="audit-logs" element={<AuditLogsPage />} />
            <Route path="settings" element={<SettingsPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>
)
