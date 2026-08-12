import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { MemoryRouter, Route, Routes, useLocation, useNavigationType } from 'react-router-dom';
import KeysPage from '../KeysPage';

function RedirectTarget() {
  const location = useLocation();
  const navigationType = useNavigationType();
  return <div>{navigationType}:{location.pathname}{location.search}</div>;
}

describe('KeysPage', () => {
  it('replaces the legacy CRUD route with the read-only compatibility workspace', async () => {
    render(
      <MemoryRouter initialEntries={['/keys']}>
        <Routes>
          <Route path="/keys" element={<KeysPage />} />
          <Route path="/applications" element={<RedirectTarget />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText('REPLACE:/applications?tab=legacy')).toBeTruthy();
  });
});
