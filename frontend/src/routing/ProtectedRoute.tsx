import React from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Button, Spin } from '@douyinfe/semi-ui'
import { profileNeedsRefresh, useAuthStore } from '../store/auth'
import { captureXInsightLoginContinuation, continueXInsightAfterProfile, hasXInsightLoginContinuation } from './xInsightLoginContinuation'

function RouteLoading() {
  return (
    <div className="ur-route-loading" role="status" aria-label="正在加载页面">
      <Spin size="large" />
      <span>正在加载页面…</span>
    </div>
  )
}

export default function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const user = useAuthStore((s) => s.user)
  const profileLoading = useAuthStore((s) => s.profileLoading)
  const profileError = useAuthStore((s) => s.profileError)
  const loadProfile = useAuthStore((s) => s.loadProfile)
  const profileNeedsLoad = profileNeedsRefresh(user)

  // Layout capture runs before the startup /me request. An incomplete profile
  // already blocks children; restore the fragment before the wizard mounts.
  React.useLayoutEffect(() => {
    if (!isAuthenticated || profileNeedsLoad) captureXInsightLoginContinuation()
    if (isAuthenticated && !profileNeedsLoad && hasXInsightLoginContinuation() && user) {
      navigate(continueXInsightAfterProfile(user), { replace: true })
    }
  }, [isAuthenticated, profileNeedsLoad, user, navigate, location.pathname, location.search, location.hash])

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
  if (profileNeedsLoad || hasXInsightLoginContinuation()) return <RouteLoading />
  return <>{children}</>
}
