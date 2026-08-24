import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from '../../test/MemoryRouter'
import LoginPage from '../../pages/LoginPage'
import { pasteValue } from '../../test/user'

// Mock the auth store
const mocks = vi.hoisted(() => ({
  mockLogin: vi.fn(),
  mockLoadProfile: vi.fn(),
  mockGetOIDCConfig: vi.fn(),
  mockGetOIDCStartURL: vi.fn(() => '/api/v1/auth/oidc/start?redirect=%2F'),
}))
const { mockLogin, mockLoadProfile, mockGetOIDCConfig, mockGetOIDCStartURL } = mocks
vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ login: mocks.mockLogin, loadProfile: mocks.mockLoadProfile }),
}))

vi.mock('../../api/client', () => ({
  api: {
    getOIDCConfig: mocks.mockGetOIDCConfig,
    getOIDCStartURL: mocks.mockGetOIDCStartURL,
  },
}))

// Mock react-router-dom navigate
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

function renderLoginPage() {
  return render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>
  )
}

describe('LoginPage', () => {
  beforeEach(() => {
    mockLogin.mockReset()
    mockLoadProfile.mockReset()
    mockLoadProfile.mockResolvedValue({ id: 1, username: 'admin', organization_role: 'org_admin', role: 'org_admin' })
    mockGetOIDCConfig.mockReset()
    mockGetOIDCStartURL.mockClear()
    mockGetOIDCStartURL.mockReturnValue('/api/v1/auth/oidc/start?redirect=%2F')
    mockGetOIDCConfig.mockReturnValue(new Promise(() => {}))
    mockNavigate.mockReset()
  })

  it('renders login form', () => {
    renderLoginPage()
    expect(screen.getByText('UniRouter')).toBeInTheDocument()
    expect(screen.getByText('Enterprise Intelligence Foundation')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('请输入用户名')).toHaveAttribute('autocomplete', 'username')
    expect(screen.getByPlaceholderText('请输入密码')).toHaveAttribute('autocomplete', 'current-password')
    expect(screen.queryByText('2.24M')).not.toBeInTheDocument()
    expect(screen.queryByText('99.92%')).not.toBeInTheDocument()
    expect(screen.queryByText('18')).not.toBeInTheDocument()
  })

  it('toggles password visibility without changing the submitted value', async () => {
    const user = userEvent.setup()
    mockLogin.mockResolvedValueOnce({ id: 1, username: 'admin', role: 'admin' })
    renderLoginPage()

    const usernameInput = screen.getByPlaceholderText('请输入用户名')
    const passwordInput = screen.getByPlaceholderText('请输入密码')
    expect(passwordInput).toHaveAttribute('type', 'password')

    await pasteValue(user, usernameInput, 'admin')
    await pasteValue(user, passwordInput, 'secret123')
    await user.click(screen.getByRole('button', { name: '显示密码' }))
    expect(passwordInput).toHaveAttribute('type', 'text')

    await user.click(screen.getByRole('button', { name: '隐藏密码' }))
    expect(passwordInput).toHaveAttribute('type', 'password')

    await user.click(screen.getByRole('button', { name: /登录/i }))

    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith('admin', 'secret123')
    })
  })

  it('shows validation errors for empty fields', async () => {
    const user = userEvent.setup()
    renderLoginPage()

    const submitBtn = screen.getByRole('button', { name: /登录/i })
    await user.click(submitBtn)

    await waitFor(() => {
      expect(screen.getByText('请输入用户名')).toBeInTheDocument()
      expect(screen.getByText('请输入密码')).toBeInTheDocument()
    })
  })

  it('calls login and navigates on success', async () => {
    mockLogin.mockResolvedValueOnce({ id: 1, username: 'admin', role: 'org_admin' })
    mockLoadProfile.mockResolvedValueOnce({ id: 1, username: 'admin', organization_role: 'org_admin', role: 'org_admin' })
    const user = userEvent.setup()
    renderLoginPage()

    const usernameInput = screen.getByPlaceholderText('请输入用户名')
    const passwordInput = screen.getByPlaceholderText('请输入密码')
    const submitBtn = screen.getByRole('button', { name: /登录/i })

    await user.type(usernameInput, 'admin')
    await user.type(passwordInput, '***')
    await user.click(submitBtn)

    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith('admin', '***')
      expect(mockLoadProfile).toHaveBeenCalledTimes(1)
      expect(mockNavigate).toHaveBeenCalledWith('/')
    })
  })

  it('waits for the authoritative profile before routing a live project member', async () => {
    mockLogin.mockResolvedValueOnce({ id: 2, username: 'dev', role: 'developer' })
    mockLoadProfile.mockResolvedValueOnce({
      id: 2, username: 'dev', organization_role: 'member', role: 'member',
      navigation_capabilities: {
        has_live_project_membership: true,
        has_project_admin_membership: false,
        can_discover_access_review: false,
        can_discover_workloads: false,
      },
    })
    const user = userEvent.setup()
    renderLoginPage()

    await user.type(screen.getByPlaceholderText('请输入用户名'), 'dev')
    await user.type(screen.getByPlaceholderText('请输入密码'), '***')
    await user.click(screen.getByRole('button', { name: /登录/i }))

    await waitFor(() => {
      expect(mockLoadProfile).toHaveBeenCalledTimes(1)
      expect(mockNavigate).toHaveBeenCalledWith('/developer')
    })
  })

  it('does not send org viewers to API docs after login', async () => {
    mockLogin.mockResolvedValueOnce({ id: 3, username: 'readonly', role: 'org_viewer' })
    mockLoadProfile.mockResolvedValueOnce({ id: 3, username: 'readonly', organization_role: 'org_viewer', role: 'org_viewer' })
    const user = userEvent.setup()
    renderLoginPage()

    await user.type(screen.getByPlaceholderText('请输入用户名'), 'readonly')
    await user.type(screen.getByPlaceholderText('请输入密码'), '***')
    await user.click(screen.getByRole('button', { name: /登录/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/')
    })
  })

  it('hands a transient profile failure to the protected retry gate', async () => {
    mockLogin.mockResolvedValueOnce({ id: 2, username: 'dev', role: 'member' })
    mockLoadProfile.mockRejectedValueOnce(new Error('profile temporarily unavailable'))
    const user = userEvent.setup()
    renderLoginPage()

    await user.type(screen.getByPlaceholderText('请输入用户名'), 'dev')
    await user.type(screen.getByPlaceholderText('请输入密码'), '***')
    await user.click(screen.getByRole('button', { name: /登录/i }))

    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/'))
    expect(screen.queryByText('profile temporarily unavailable')).toBeNull()
  })

  it('shows error message on login failure', async () => {
    mockLogin.mockRejectedValueOnce(new Error('Invalid credentials'))
    const user = userEvent.setup()
    renderLoginPage()

    const usernameInput = screen.getByPlaceholderText('请输入用户名')
    const passwordInput = screen.getByPlaceholderText('请输入密码')
    const submitBtn = screen.getByRole('button', { name: /登录/i })

    await user.type(usernameInput, 'admin')
    await user.type(passwordInput, 'wrong')
    await user.click(submitBtn)

    await waitFor(() => {
      expect(screen.getByText('Invalid credentials')).toBeInTheDocument()
    })
  })

  it('shows default error message when error has no message', async () => {
    mockLogin.mockRejectedValueOnce(new Error())
    const user = userEvent.setup()
    renderLoginPage()

    const usernameInput = screen.getByPlaceholderText('请输入用户名')
    const passwordInput = screen.getByPlaceholderText('请输入密码')
    const submitBtn = screen.getByRole('button', { name: /登录/i })

    await user.type(usernameInput, 'admin')
    await user.type(passwordInput, 'wrong')
    await user.click(submitBtn)

    await waitFor(() => {
      expect(screen.getByText('登录失败')).toBeInTheDocument()
    })
  })

  it('renders OIDC login button when enabled', async () => {
    mockGetOIDCConfig.mockResolvedValueOnce({ enabled: true, provider_name: 'Acme IDP' })
    renderLoginPage()

    expect(await screen.findByRole('button', { name: /Acme IDP/ })).toBeInTheDocument()
  })

  it('shows an indeterminate enterprise-login state and supports retry when config loading fails', async () => {
    mockGetOIDCConfig.mockRejectedValueOnce(new Error('network unavailable'))
    renderLoginPage()

    expect(await screen.findByText(/企业登录配置暂时无法确认/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Acme IDP/ })).toBeNull()

    mockGetOIDCConfig.mockResolvedValueOnce({ enabled: true, provider_name: 'Acme IDP' })
    fireEvent.click(screen.getByRole('button', { name: '重新检查企业登录' }))

    expect(await screen.findByRole('button', { name: /Acme IDP/ })).toBeInTheDocument()
    expect(mockGetOIDCConfig).toHaveBeenCalledTimes(2)
  })
})
