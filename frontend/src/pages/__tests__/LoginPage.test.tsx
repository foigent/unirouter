import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import LoginPage from '../../pages/LoginPage'

// Mock the auth store
const mocks = vi.hoisted(() => ({
  mockLogin: vi.fn(),
  mockGetOIDCConfig: vi.fn(),
  mockGetOIDCStartURL: vi.fn(() => '/api/v1/auth/oidc/start?redirect=%2F'),
}))
const { mockLogin, mockGetOIDCConfig, mockGetOIDCStartURL } = mocks
vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ login: mocks.mockLogin }),
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
  })

  it('toggles password visibility without changing the submitted value', async () => {
    mockLogin.mockResolvedValueOnce({ id: 1, username: 'admin', role: 'admin' })
    renderLoginPage()

    const usernameInput = screen.getByPlaceholderText('请输入用户名')
    const passwordInput = screen.getByPlaceholderText('请输入密码')
    expect(passwordInput).toHaveAttribute('type', 'password')

    fireEvent.change(usernameInput, { target: { value: 'admin' } })
    fireEvent.change(passwordInput, { target: { value: 'secret123' } })
    fireEvent.click(screen.getByRole('button', { name: '显示密码' }))
    expect(passwordInput).toHaveAttribute('type', 'text')

    fireEvent.click(screen.getByRole('button', { name: '隐藏密码' }))
    expect(passwordInput).toHaveAttribute('type', 'password')

    fireEvent.click(screen.getByRole('button', { name: /登录/i }))

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
      expect(mockNavigate).toHaveBeenCalledWith('/')
    })
  })

  it('navigates project users to developer console after login', async () => {
    mockLogin.mockResolvedValueOnce({ id: 2, username: 'dev', role: 'developer' })
    const user = userEvent.setup()
    renderLoginPage()

    await user.type(screen.getByPlaceholderText('请输入用户名'), 'dev')
    await user.type(screen.getByPlaceholderText('请输入密码'), '***')
    await user.click(screen.getByRole('button', { name: /登录/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/developer')
    })
  })

  it('does not send org viewers to API docs after login', async () => {
    mockLogin.mockResolvedValueOnce({ id: 3, username: 'readonly', role: 'org_viewer' })
    const user = userEvent.setup()
    renderLoginPage()

    await user.type(screen.getByPlaceholderText('请输入用户名'), 'readonly')
    await user.type(screen.getByPlaceholderText('请输入密码'), '***')
    await user.click(screen.getByRole('button', { name: /登录/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/access')
    })
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
})
