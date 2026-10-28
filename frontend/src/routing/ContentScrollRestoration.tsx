import { useLayoutEffect, useRef, type ReactNode } from 'react';
import { useLocation, useNavigationType } from 'react-router-dom';

// The shell scrolls independently of window. Keep positions in this App session,
// without putting identity or UI state in browser history or persistent storage.
export default function ContentScrollRestoration({ session, children }: { session: string; children: ReactNode }) {
  const location = useLocation();
  const navigation = useNavigationType();
  const root = useRef<HTMLDivElement>(null);
  const cache = useRef({ session, entry: '', view: '', hash: '', positions: new Map<string, number>() });
  const params = new URLSearchParams(location.search);
  const view = JSON.stringify([location.pathname, params.get('view'), params.get('tab'), params.get('section')]);

  useLayoutEffect(() => {
    const content = root.current?.closest<HTMLElement>('.ur-content');
    if (!content) return;
    const saved = cache.current;
    const sameSession = saved.session === session;
    const anchorRequested = Boolean(location.hash && sameSession &&
      !(navigation === 'POP' && saved.positions.has(location.key)) &&
      (location.hash !== saved.hash || view !== saved.view));
    const position = !sameSession ? 0 : navigation === 'POP'
      ? saved.positions.get(location.key) ?? 0
      : saved.view === view ? saved.positions.get(saved.entry) ?? content.scrollTop : 0;
    if (!sameSession) saved.positions.clear();
    Object.assign(saved, { session, entry: location.key, view, hash: location.hash });
    saved.positions.set(location.key, position);
    let pending = position > 0;
    let restoring = true;
    let observer: ResizeObserver | undefined;
    const restore = () => {
      if (!restoring) return;
      if (anchorRequested) {
        let id = location.hash.slice(1);
        try { id = decodeURIComponent(id); } catch { /* Keep malformed raw IDs inspectable. */ }
        const target = document.getElementById(id);
        pending = !target || !root.current?.contains(target);
        if (!pending) { target?.scrollIntoView({ block: 'nearest' }); saved.positions.set(location.key, content.scrollTop); }
      } else {
        content.scrollTop = position;
        pending = Math.abs(content.scrollTop - position) > 1;
      }
      if (!pending) { restoring = false; observer?.disconnect(); }
    };
    const remember = () => { if (!pending) saved.positions.set(location.key, content.scrollTop); };
    const cancelPending = () => { restoring = false; pending = false; observer?.disconnect(); remember(); };
    const onKey = (event: KeyboardEvent) => {
      if (['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', 'Home', 'End', ' '].includes(event.key)) cancelPending();
    };
    restore();
    // A lazy page can mount before its data. Restore again when its real content
    // has room, stopping immediately when the user takes control of scrolling.
    if (pending && typeof ResizeObserver !== 'undefined' && root.current) {
      observer = new ResizeObserver(restore);
      observer.observe(root.current);
    }
    content.addEventListener('scroll', remember);
    content.addEventListener('wheel', cancelPending, { passive: true });
    content.addEventListener('touchstart', cancelPending, { passive: true });
    content.addEventListener('pointerdown', cancelPending);
    content.addEventListener('keydown', onKey);
    return () => {
      restoring = false;
      observer?.disconnect();
      content.removeEventListener('scroll', remember);
      content.removeEventListener('wheel', cancelPending);
      content.removeEventListener('touchstart', cancelPending);
      content.removeEventListener('pointerdown', cancelPending);
      content.removeEventListener('keydown', onKey);
    };
  }, [location.key, location.hash, navigation, session, view]);

  return <div ref={root} className="ur-route-content">{children}</div>;
}
