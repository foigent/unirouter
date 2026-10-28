import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Route, Routes, useNavigate } from 'react-router-dom';
import { MemoryRouter } from '../../test/MemoryRouter';
import ContentScrollRestoration from '../ContentScrollRestoration';

function Controls() {
  const navigate = useNavigate();
  return <><button onClick={() => navigate('/usage')}>Usage</button><button onClick={() => navigate('/cost?project=2')}>Filter</button><button onClick={() => navigate('/cost?tab=details')}>Details tab</button><button onClick={() => navigate(-1)}>Back</button><button onClick={() => navigate(1)}>Forward</button><button onClick={() => navigate('/cost#section')}>Section</button><h2 id="section">Subsection</h2><Routes><Route path="cost" element={<h1>Cost</h1>} /><Route path="usage" element={<h1>Usage data</h1>} /></Routes></>;
}
const tree = (session = 'actor-a') => <MemoryRouter initialEntries={['/cost']}><div className="ur-content" data-testid="scroll"><ContentScrollRestoration session={session}><Controls /></ContentScrollRestoration></div></MemoryRouter>;
const scrollTo = (value: number) => { const el = screen.getByTestId('scroll'); el.scrollTop = value; fireEvent.scroll(el); return el; };
afterEach(() => vi.unstubAllGlobals());
describe('shell navigation scroll', () => {
  it('starts a new task at the top and restores the previous history entry', async () => {
    render(tree()); const el = scrollTo(850);
    fireEvent.click(screen.getByRole('button', { name: 'Usage' }));
    await screen.findByRole('heading', { name: 'Usage data' }); expect(el.scrollTop).toBe(0);
    scrollTo(120); fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    await screen.findByRole('heading', { name: 'Cost' }); expect(el.scrollTop).toBe(850);
    fireEvent.click(screen.getByRole('button', { name: 'Forward' }));
    await screen.findByRole('heading', { name: 'Usage data' }); expect(el.scrollTop).toBe(120);
  });
  it('retains the position for filters but resets when changing the primary view', async () => {
    render(tree()); const el = scrollTo(340);
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    await waitFor(() => expect(el.scrollTop).toBe(340));
    fireEvent.click(screen.getByRole('button', { name: 'Details tab' }));
    await waitFor(() => expect(el.scrollTop).toBe(0));
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    await waitFor(() => expect(el.scrollTop).toBe(340));
  });
  it('waits for asynchronously loaded content to have enough height', async () => {
    let resized = () => {};
    vi.stubGlobal('ResizeObserver', class { constructor(cb: () => void) { resized = cb; } observe() {} disconnect() {} });
    render(tree()); const el = scrollTo(600);
    fireEvent.click(screen.getByRole('button', { name: 'Usage' }));
    await screen.findByRole('heading', { name: 'Usage data' });
    let max = 0, top = 0;
    Object.defineProperty(el, 'scrollTop', { configurable: true, get: () => top, set: value => { top = Math.min(value, max); } });
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    await screen.findByRole('heading', { name: 'Cost' }); expect(el.scrollTop).toBe(0);
    max = 1000; act(() => resized()); expect(el.scrollTop).toBe(600);
  });
  it('lets the user cancel a pending restoration', async () => {
    let resized = () => {};
    vi.stubGlobal('ResizeObserver', class { constructor(cb: () => void) { resized = cb; } observe() {} disconnect() {} });
    render(tree()); const el = scrollTo(600);
    fireEvent.click(screen.getByRole('button', { name: 'Usage' })); await screen.findByRole('heading', { name: 'Usage data' });
    let max = 0, top = 0;
    Object.defineProperty(el, 'scrollTop', { configurable: true, get: () => top, set: value => { top = Math.min(value, max); } });
    fireEvent.click(screen.getByRole('button', { name: 'Back' })); await screen.findByRole('heading', { name: 'Cost' });
    fireEvent.wheel(el); max = 1000; act(() => resized()); expect(el.scrollTop).toBe(0);
  });
  it('clears previous actor/session positions', async () => {
    const view = render(tree()); const el = scrollTo(500);
    view.rerender(tree('actor-b')); expect(el.scrollTop).toBe(0);
    fireEvent.click(screen.getByRole('button', { name: 'Usage' })); await screen.findByRole('heading', { name: 'Usage data' });
    fireEvent.click(screen.getByRole('button', { name: 'Back' })); await screen.findByRole('heading', { name: 'Cost' }); expect(el.scrollTop).toBe(0);
  });
  it('honors an explicit anchor without overwriting it with a saved offset', async () => {
    render(tree()); const el = scrollTo(250);
    const target = screen.getByRole('heading', { name: 'Subsection' });
    const intoView = vi.fn(() => { el.scrollTop = 480; });
    Object.defineProperty(target, 'scrollIntoView', { configurable: true, value: intoView });
    fireEvent.click(screen.getByRole('button', { name: 'Section' }));
    await waitFor(() => expect(intoView).toHaveBeenCalledWith({ block: 'nearest' }));
    expect(el.scrollTop).toBe(480);
  });

});
