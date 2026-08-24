import { render, screen } from '@testing-library/react';
import { Navigate, Outlet, Route, Routes, useLocation, useNavigationType } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { MemoryRouter } from '../../test/MemoryRouter';
import { ROUTER_FUTURE_FLAGS } from '../routerFuture';

function LocationProbe() {
  const location = useLocation();
  const navigationType = useNavigationType();
  return (
    <output aria-label="current route">
      {location.pathname}{location.search}{location.hash} · {navigationType}
    </output>
  );
}

describe('router future compatibility', () => {
  it('opts production and test routers into both React Router v7 behaviors', () => {
    expect(ROUTER_FUTURE_FLAGS).toEqual({
      v7_startTransition: true,
      v7_relativeSplatPath: true,
    });
  });

  it('restores an absolute deep link including query and hash', () => {
    render(
      <MemoryRouter initialEntries={['/known?from=refresh#details']}>
        <Routes>
          <Route path="/known" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByRole('status', { name: 'current route' })).toHaveTextContent(
      '/known?from=refresh#details · POP',
    );
  });

  it('keeps the catch-all route on an absolute replace redirect', () => {
    render(
      <MemoryRouter initialEntries={['/missing/deep/path']}>
        <Routes>
          <Route path="/" element={<Outlet />}>
            <Route path="known" element={<LocationProbe />} />
            <Route path="*" element={<Navigate to="/known?from=catch-all" replace />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByRole('status', { name: 'current route' })).toHaveTextContent(
      '/known?from=catch-all · REPLACE',
    );
  });
});
