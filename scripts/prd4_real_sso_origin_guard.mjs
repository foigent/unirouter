export class CredentialOriginError extends Error {
  constructor(message) {
    super(message);
    this.name = 'CredentialOriginError';
  }
}

export function buildAllowedCredentialOrigins(authorizationEndpoint, additionalOrigins = '') {
  const endpoint = parseAbsoluteURL(authorizationEndpoint, 'OIDC authorization endpoint');
  if (endpoint.protocol !== 'https:') {
    throw new CredentialOriginError('OIDC authorization endpoint must use HTTPS');
  }
  rejectURLCredentials(endpoint, 'OIDC authorization endpoint');

  const origins = new Set([endpoint.origin]);
  for (const rawOrigin of String(additionalOrigins || '').split(',').map(value => value.trim()).filter(Boolean)) {
    const configured = parseAbsoluteURL(rawOrigin, 'additional credential origin');
    if (configured.protocol !== 'https:') {
      throw new CredentialOriginError('additional credential origins must use HTTPS');
    }
    if (configured.username || configured.password ||
        (configured.pathname !== '' && configured.pathname !== '/') ||
        configured.search || configured.hash) {
      throw new CredentialOriginError('additional credential origins must be exact origins without credentials, paths, queries, or fragments');
    }
    origins.add(configured.origin);
  }
  return origins;
}

export function canonicalNavigationOrigin(rawURL, label) {
  const parsed = parseAbsoluteURL(rawURL, label);
  if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
    throw new CredentialOriginError(`${label} must use HTTP or HTTPS`);
  }
  rejectURLCredentials(parsed, label);
  return parsed.origin;
}

export function requireCredentialOrigin(rawURL, allowedOrigins, label) {
  let current;
  try {
    current = new URL(rawURL);
  } catch {
    throw new CredentialOriginError(`refusing to fill ${label} on a malformed browser URL`);
  }
  if (current.username || current.password) {
    throw new CredentialOriginError(`refusing to fill ${label} on a browser URL containing userinfo`);
  }
  if (current.protocol !== 'https:' || !allowedOrigins.has(current.origin)) {
    throw new CredentialOriginError(`refusing to fill ${label} on unapproved origin ${current.origin}`);
  }
  return current.origin;
}

export async function installMainFrameNavigationGuard(page, allowedOrigins) {
  let violation = null;
  let currentAllowedOrigins = new Set(allowedOrigins);
  const routeHandler = async route => {
    const request = route.request();
    if (!request.isNavigationRequest() || request.frame() !== page.mainFrame()) {
      await route.continue();
      return;
    }

    let requested;
    try {
      requested = new URL(request.url());
    } catch {
      violation = new CredentialOriginError('refusing malformed main-frame navigation URL during SSO credential entry');
      await route.abort('blockedbyclient');
      return;
    }
    if (requested.username || requested.password) {
      violation = new CredentialOriginError('refusing main-frame navigation URL containing userinfo during SSO credential entry');
      await route.abort('blockedbyclient');
      return;
    }
    const requestedOrigin = requested.origin;
    if (!currentAllowedOrigins.has(requestedOrigin)) {
      violation = new CredentialOriginError(`refusing unapproved main-frame navigation to origin ${requestedOrigin} during SSO credential entry`);
      await route.abort('blockedbyclient');
      return;
    }
    await route.continue();
  };

  await page.route('**/*', routeHandler);
  return {
    assertAllowed() {
      if (violation) throw violation;
    },
    setAllowedOrigins(nextAllowedOrigins) {
      if (violation) throw violation;
      currentAllowedOrigins = new Set(nextAllowedOrigins);
    },
    async dispose() {
      await page.unroute('**/*', routeHandler);
    },
  };
}

export async function fillFirstVisible(page, selectors, valueProvider, label, allowedOrigins, navigationGuard) {
  const deadline = Date.now() + 30000;
  let lastError = '';
  while (Date.now() < deadline) {
    navigationGuard?.assertAllowed();
    for (const selector of selectors) {
      const locator = page.locator(selector).first();
      try {
        await locator.waitFor({ state: 'visible', timeout: 500 });
        navigationGuard?.assertAllowed();
        requireCredentialOrigin(page.url(), allowedOrigins, label);
        const value = typeof valueProvider === 'function' ? valueProvider() : valueProvider;
        await locator.fill(value);
        return;
      } catch (err) {
        if (err instanceof CredentialOriginError) throw err;
        navigationGuard?.assertAllowed();
        lastError = err instanceof Error ? err.name : 'field interaction failed';
      }
    }
    await delay(250);
  }
  throw new Error(`could not find visible ${label} field; last error=${lastError}`);
}

function parseAbsoluteURL(rawURL, label) {
  try {
    return new URL(rawURL);
  } catch {
    throw new CredentialOriginError(`${label} must be an absolute URL`);
  }
}

function rejectURLCredentials(parsed, label) {
  if (parsed.username || parsed.password) {
    throw new CredentialOriginError(`${label} must not contain userinfo`);
  }
}

function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}
