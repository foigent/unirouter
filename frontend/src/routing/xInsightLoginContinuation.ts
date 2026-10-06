import { authzContextForUser, getDefaultPathForAuthz, hasLicensedFeature, isRouteAllowedForAuthz } from '../navigation';
import type { UserProfile } from '../store/auth';

const installPath = '/connectors/x-insight/install';
const recoveryPath = '/connectors/x-insight/recover';
// One document, one fixed purpose. Never persist the invitation or accept a
// caller-supplied return URL. This is a handoff, not authorization evidence.
let pending: { fragment: string; path: string; phase: 'awaiting-auth' | 'returning' } | undefined;

export function isXInsightRecoveryLocation(pathname: string, search: string, hash: string) {
  if (pathname !== recoveryPath || search !== '') return false;
  const fields = new URLSearchParams(hash.replace(/^#/, ''));
  if ([...fields.keys()].length !== 4 || fields.getAll('xinsight_origin').length !== 1 ||
      fields.getAll('installation_id').length !== 1 || fields.getAll('recovery_id').length !== 1 ||
      fields.getAll('recovery_ticket').length !== 1 ||
      !/^xci_[A-Za-z0-9_-]{22}$/.test(fields.get('installation_id') || '') ||
      !/^xrr_[A-Za-z0-9_-]{22}$/.test(fields.get('recovery_id') || '') ||
      !/^xirt_[A-Za-z0-9_-]{43}$/.test(fields.get('recovery_ticket') || '')) return false;
  try {
    const origin = fields.get('xinsight_origin') || '';
    const url = new URL(origin);
    return url.protocol === 'https:' && !url.username && !url.password && url.origin === origin;
  } catch { return false; }
}

export function isXInsightInvitationLocation(pathname: string, search: string, hash: string) {
  if (!((pathname === installPath && search === '') || (pathname === '/integrations' && search === '?tab=xinsight'))) return false;
  const fields = new URLSearchParams(hash.replace(/^#/, ''));
  if ([...fields.keys()].length !== 2 || fields.getAll('invitation').length !== 1 || fields.getAll('xinsight_origin').length !== 1 || !/^xiiv1_[A-Za-z0-9_-]{43}$/.test(fields.get('invitation') || '')) return false;
  try {
    const origin = fields.get('xinsight_origin') || '';
    const url = new URL(origin);
    return url.protocol === 'https:' && !url.username && !url.password && url.origin === origin;
  } catch { return false; }
}

export function captureXInsightLoginContinuation() {
  const { pathname, search, hash } = window.location;
  if (!isXInsightInvitationLocation(pathname, search, hash) && !isXInsightRecoveryLocation(pathname, search, hash)) return;
  pending = { fragment: hash, path: pathname === recoveryPath ? recoveryPath : installPath, phase: 'awaiting-auth' };
  window.history.replaceState(null, '', `${pathname}${search}`);
}

export function hasXInsightLoginContinuation() { return pending?.phase === 'awaiting-auth'; }

export function continueXInsightAfterProfile(user: UserProfile) {
  const authz = authzContextForUser(user);
  const fallback = getDefaultPathForAuthz(authz);
  if (!hasXInsightLoginContinuation()) return fallback;
  if (!isRouteAllowedForAuthz(pending!.path, authz) || !hasLicensedFeature(user.license_access, 'data_exchange')) {
    pending = undefined;
    return fallback;
  }
  pending!.phase = 'returning';
  return `${pending!.path}${pending!.fragment}`;
}

export function finishXInsightLoginContinuation(fragment: string) {
  if (pending?.phase === 'returning' && pending.fragment === fragment) pending = undefined;
}

export function clearXInsightLoginContinuation() { pending = undefined; }

export function preservesXInsightLoginOn401(path: string, method: string, version: string) {
  if (!hasXInsightLoginContinuation() || version !== 'v1') return false;
  const onLogin = window.location.pathname === '/login';
  const onStartup = window.location.pathname === installPath || window.location.pathname === recoveryPath || (window.location.pathname === '/integrations' && window.location.search === '?tab=xinsight');
  return (path === '/me' && method === 'GET' && (onStartup || onLogin)) || (path === '/auth/login' && method === 'POST' && onLogin);
}
