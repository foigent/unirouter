import { lazy, Suspense } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ErrorBoundary from './ErrorBoundary';
import { setLocale } from '../i18n';

function BrokenPage(): never {
  throw new Error('internal SQL and token details');
}

describe('ErrorBoundary', () => {
  beforeEach(() => setLocale('zh-CN'));
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    localStorage.removeItem('token');
  });

  it('logs diagnostics without exposing the raw exception to the user', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);

    render(<ErrorBoundary><BrokenPage /></ErrorBoundary>);

    expect(screen.getByText('页面出错了')).toBeTruthy();
    expect(screen.getByText(/页面暂时无法继续显示/)).toBeTruthy();
    expect(screen.queryByText(/internal SQL and token details/)).toBeNull();
    expect(screen.getByRole('button', { name: '返回首页' })).toBeInTheDocument();
  });

  it.each([
    'Failed to fetch dynamically imported module: https://private.invalid/assets/missing.js',
    'Importing a module script failed.',
    'error loading dynamically imported module: https://private.invalid/assets/missing.js',
    'Unable to preload CSS for /assets/missing.css',
  ])('offers manual recovery for a rejected lazy resource: %s', async (message) => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const reload = mockReload();
    const LazyPage = lazy(() => Promise.reject(new TypeError(message)));

    render(<ErrorBoundary><Suspense fallback={<p>Loading</p>}><LazyPage /></Suspense></ErrorBoundary>);

    expect(await screen.findByRole('heading', { name: '页面资源加载失败' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重新加载页面' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '返回首页' })).toBeNull();
    expect(screen.queryByText(message)).toBeNull();
    expect(reload).not.toHaveBeenCalled();
  });

  it('switches the resource fallback to English and reloads only after a click, retaining the local session', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const reload = mockReload();
    localStorage.setItem('token', 'synthetic-local-test-session');
    const LazyPage = lazy(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module: https://private.invalid/assets/missing.js')));
    render(<ErrorBoundary><Suspense fallback={<p>Loading</p>}><LazyPage /></Suspense></ErrorBoundary>);

    await screen.findByRole('heading', { name: '页面资源加载失败' });
    fireEvent.click(screen.getByRole('button', { name: 'English' }));
    expect(screen.getByRole('heading', { name: 'Page resources could not be loaded' })).toBeInTheDocument();
    expect(screen.getByText(/deployment update or network interruption may be involved/)).toBeInTheDocument();
    expect(screen.queryByText(/private.invalid/)).toBeNull();
    expect(reload).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Reload page' }));
    expect(reload).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem('token')).toBe('synthetic-local-test-session');
  });

  it('keeps an ordinary failed fetch in the generic fallback', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    function FailedRequest(): never { throw new TypeError('Failed to fetch'); }
    render(<ErrorBoundary><FailedRequest /></ErrorBoundary>);
    expect(screen.getByRole('heading', { name: '页面出错了' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '页面资源加载失败' })).toBeNull();
  });

  it('renders healthy children normally', () => {
    render(<ErrorBoundary><h1>Healthy page</h1></ErrorBoundary>);
    expect(screen.getByRole('heading', { name: 'Healthy page' })).toBeInTheDocument();
    expect(screen.queryByRole('alert')).toBeNull();
  });
});

function mockReload() {
  const reload = vi.fn();
  const actualWindow = window;
  vi.stubGlobal('window', new Proxy(actualWindow, {
    get(target, key) {
      return key === 'location' ? { reload } : Reflect.get(target, key, target);
    },
  }));
  return reload;
}
