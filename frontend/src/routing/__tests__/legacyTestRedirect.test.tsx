import { act, screen } from '@testing-library/react';
import ReactDOM, { type Root } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../App', async () => ({ default: (await import('react-router-dom')).Outlet }));
vi.mock('../../pages/ModelEndpointsPage', () => ({ default: () => <h1>Model endpoints</h1> }));
vi.mock('../../pages/LoginPage', () => ({ default: () => <h1>Sign in</h1> }));

const createRoot = ReactDOM.createRoot;
let mountedRoot: Root | undefined;

async function openConsole(path: string, isAuthenticated: boolean) {
  vi.resetModules();
  const { useAuthStore } = await import('../../store/auth');
  useAuthStore.setState({
    isAuthenticated,
    user: isAuthenticated ? {
      id: 1,
      username: 'route-test-admin',
      role: 'platform_admin',
      organization_role: 'platform_admin',
      identity_scope: 'deployment_operator',
      customer_context: null,
      license_access: null,
      navigation_capabilities: null,
    } : null,
  });

  window.history.replaceState(null, '', path);
  const container = document.createElement('div');
  container.id = 'root';
  document.body.append(container);
  vi.spyOn(ReactDOM, 'createRoot').mockImplementation((element, options) => {
    mountedRoot = createRoot(element, options);
    return mountedRoot;
  });

  await act(async () => {
    await import('../../main');
  });
}

afterEach(async () => {
  const root = mountedRoot;
  if (root) await act(() => root.unmount());
  mountedRoot = undefined;
  document.getElementById('root')?.remove();
  window.history.replaceState(null, '', '/');
  vi.restoreAllMocks();
});

describe('the console entrypoint legacy detection route', () => {
  it.each([
    '/test',
    '/test?channel_id=8&model=deepseek-v4-pro#diagnostics',
    '/test?channel_id=invalid',
  ])('replaces %s with the model endpoints list', async (path) => {
    const historyLength = window.history.length;
    await openConsole(path, true);

    expect(await screen.findByRole('heading', { name: 'Model endpoints' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/channels');
    expect(window.location.search).toBe('');
    expect(window.location.hash).toBe('');
    expect(window.history.length).toBe(historyLength);
  });

  it('requires sign-in at the legacy detection URL', async () => {
    await openConsole('/test?channel_id=8&model=deepseek-v4-pro', false);

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/login');
  });
});
