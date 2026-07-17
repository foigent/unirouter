import assert from 'node:assert/strict';
import test from 'node:test';
import {
  CredentialOriginError,
  buildAllowedCredentialOrigins,
  fillFirstVisible,
  installMainFrameNavigationGuard,
  requireCredentialOrigin,
} from './prd4_real_sso_origin_guard.mjs';

const fakeUsername = 'mock-user@example.test';
const fakePassword = 'FAKE_ONLY_not_a_secret_42';

test('authorization endpoint and explicitly configured HTTPS origins are canonicalized', () => {
  const origins = buildAllowedCredentialOrigins(
    'https://idp.example.test/oauth/authorize?client_id=test',
    'https://federation.example.test/, https://idp.example.test:8443',
  );
  assert.deepEqual([...origins], [
    'https://idp.example.test',
    'https://federation.example.test',
    'https://idp.example.test:8443',
  ]);
});

test('default ports and internationalized domains use canonical URL origins', () => {
  const defaultPortOrigins = buildAllowedCredentialOrigins(
    'https://idp.example.test:443/oauth/authorize',
    'https://federation.example.test:443/',
  );
  assert.deepEqual([...defaultPortOrigins], [
    'https://idp.example.test',
    'https://federation.example.test',
  ]);

  const idnOrigins = buildAllowedCredentialOrigins('https://bücher.example/authorize');
  assert.deepEqual([...idnOrigins], ['https://xn--bcher-kva.example']);
  assert.equal(
    requireCredentialOrigin('https://bücher.example/login', idnOrigins, 'username'),
    'https://xn--bcher-kva.example',
  );
});

test('credential origins reject HTTP, malformed URLs, and configured paths', () => {
  assert.throws(
    () => buildAllowedCredentialOrigins('http://idp.example.test/authorize'),
    /authorization endpoint must use HTTPS/,
  );
  assert.throws(
    () => buildAllowedCredentialOrigins('not a URL'),
    /must be an absolute URL/,
  );
  assert.throws(
    () => buildAllowedCredentialOrigins('https://idp.example.test/authorize', 'https://federation.example.test/login'),
    /must be exact origins/,
  );
  assert.throws(
    () => buildAllowedCredentialOrigins('https://user:password@idp.example.test/authorize'),
    /must not contain userinfo/,
  );
  assert.throws(
    () => buildAllowedCredentialOrigins('https://idp.example.test/authorize', 'https://user@federation.example.test'),
    /must be exact origins/,
  );
});

test('exact approved HTTPS origin is required', () => {
  const allowed = new Set(['https://idp.example.test']);
  assert.equal(requireCredentialOrigin('https://idp.example.test/login', allowed, 'username'), 'https://idp.example.test');
  for (const rejected of [
    'http://idp.example.test/login',
    'https://idp.example.test.attacker.test/login',
    'https://idp.example.test%2eattacker.test/login',
    'https://idp.example.test:8443/login',
    'https://user@idp.example.test/login',
    'https://[::1/login',
    'not a URL',
  ]) {
    assert.throws(() => requireCredentialOrigin(rejected, allowed, 'username'), CredentialOriginError);
  }
});

test('exact approved origin resolves and fills a credential only after the check', async () => {
  const page = fakeFillPage('https://idp.example.test/authorize');
  let credentialReads = 0;
  await fillFirstVisible(
    page,
    ['input[name="username"]'],
    () => {
      credentialReads += 1;
      return fakeUsername;
    },
    'username',
    new Set(['https://idp.example.test']),
  );
  assert.equal(credentialReads, 1);
  assert.deepEqual(page.filledValues, [fakeUsername]);
});

test('unexpected username origin is rejected before reading or filling the credential', async () => {
  const page = fakeFillPage('https://unexpected.example.test/sign-in');
  let credentialReads = 0;
  await assert.rejects(
    fillFirstVisible(
      page,
      ['input[name="username"]'],
      () => {
        credentialReads += 1;
        return fakeUsername;
      },
      'username',
      new Set(['https://idp.example.test']),
    ),
    error => {
      assert.match(error.message, /unapproved origin/);
      assert.doesNotMatch(error.message, /mock-user|FAKE_ONLY/);
      return true;
    },
  );
  assert.equal(credentialReads, 0);
  assert.deepEqual(page.filledValues, []);
});

test('redirect after the field becomes visible is rejected before reading or filling the credential', async () => {
  const page = fakeFillPage('https://idp.example.test/authorize', () => {
    page.currentURL = 'https://unexpected.example.test/sign-in';
  });
  let credentialReads = 0;
  await assert.rejects(
    fillFirstVisible(
      page,
      ['input[name="username"]'],
      () => {
        credentialReads += 1;
        return fakeUsername;
      },
      'username',
      new Set(['https://idp.example.test']),
    ),
    /unapproved origin/,
  );
  assert.equal(credentialReads, 0);
  assert.deepEqual(page.filledValues, []);
});

test('cross-origin password transition is rejected before reading or filling the credential', async () => {
  const page = fakeFillPage('https://idp.example.test/authorize');
  page.currentURL = 'https://unexpected.example.test/sign-in';
  let credentialReads = 0;
  await assert.rejects(
    fillFirstVisible(
      page,
      ['input[name="password"]'],
      () => {
        credentialReads += 1;
        return fakePassword;
      },
      'password',
      new Set(['https://idp.example.test']),
    ),
    /unapproved origin/,
  );
  assert.equal(credentialReads, 0);
  assert.deepEqual(page.filledValues, []);
});

test('main-frame navigation guard aborts an unapproved redirect', async () => {
  const page = fakeRoutedPage();
  const guard = await installMainFrameNavigationGuard(page, new Set([
    'https://console.example.test',
    'https://idp.example.test',
  ]));
  const route = page.navigationRoute('https://unexpected.example.test/sign-in');
  await page.routeHandler(route);
  assert.equal(route.aborted, true);
  assert.equal(route.continued, false);
  assert.throws(() => guard.assertAllowed(), /unapproved main-frame navigation/);
  await guard.dispose();
  assert.equal(page.unrouted, true);
});

test('main-frame navigation guard rejects userinfo even on an approved origin', async () => {
  const page = fakeRoutedPage();
  const guard = await installMainFrameNavigationGuard(page, new Set(['https://idp.example.test']));
  const route = page.navigationRoute('https://user:password@idp.example.test/sign-in');
  await page.routeHandler(route);
  assert.equal(route.aborted, true);
  assert.equal(route.continued, false);
  assert.throws(() => guard.assertAllowed(), /userinfo/);
  await guard.dispose();
});

test('credential-entry guard rejects a console redirect that was allowed before credential entry', async () => {
  const page = fakeRoutedPage();
  const guard = await installMainFrameNavigationGuard(page, new Set([
    'https://console.example.test',
    'https://idp.example.test',
  ]));
  guard.setAllowedOrigins(new Set(['https://idp.example.test']));
  const route = page.navigationRoute('https://console.example.test/login');
  await page.routeHandler(route);
  assert.equal(route.aborted, true);
  assert.equal(route.continued, false);
  assert.throws(() => guard.assertAllowed(), /unapproved main-frame navigation/);
  await guard.dispose();
});

test('main-frame navigation guard preserves approved navigation and unrelated subresources', async () => {
  const page = fakeRoutedPage();
  const guard = await installMainFrameNavigationGuard(page, new Set([
    'https://console.example.test',
    'https://idp.example.test',
  ]));
  const approvedNavigation = page.navigationRoute('https://idp.example.test/authorize');
  await page.routeHandler(approvedNavigation);
  assert.equal(approvedNavigation.aborted, false);
  assert.equal(approvedNavigation.continued, true);

  const externalSubresource = page.navigationRoute('https://static.example.test/style.css', false);
  await page.routeHandler(externalSubresource);
  assert.equal(externalSubresource.aborted, false);
  assert.equal(externalSubresource.continued, true);

  const childFrameNavigation = page.navigationRoute('https://unapproved.example.test/frame', true, {});
  await page.routeHandler(childFrameNavigation);
  assert.equal(childFrameNavigation.aborted, false);
  assert.equal(childFrameNavigation.continued, true);
  assert.doesNotThrow(() => guard.assertAllowed());
  await guard.dispose();
});

test('submit requires an exact approved HTTPS origin without exposing credentials', () => {
  const allowed = new Set(['https://idp.example.test']);
  assert.equal(
    requireCredentialOrigin('https://idp.example.test/authorize', allowed, 'submit'),
    'https://idp.example.test',
  );
  for (const rejected of [
    'http://idp.example.test/authorize',
    'https://idp.example.test.attacker.test/authorize',
    'https://user@idp.example.test/authorize',
  ]) {
    assert.throws(
      () => requireCredentialOrigin(rejected, allowed, 'submit'),
      error => {
        assert.doesNotMatch(error.message, /mock-user|FAKE_ONLY/);
        return true;
      },
    );
  }
});

function fakeFillPage(initialURL, onWait = () => {}) {
  return {
    currentURL: initialURL,
    filledValues: [],
    url() {
      return this.currentURL;
    },
    locator() {
      return {
        first: () => ({
          waitFor: async () => onWait(),
          fill: async value => {
            this.filledValues.push(value);
          },
        }),
      };
    },
  };
}

function fakeRoutedPage() {
  const mainFrame = {};
  const page = {
    unrouted: false,
    routeHandler: null,
    mainFrame: () => mainFrame,
    route: async (_pattern, handler) => {
      page.routeHandler = handler;
    },
    unroute: async () => {
      page.unrouted = true;
    },
    navigationRoute(url, isNavigation = true, frame = mainFrame) {
      const request = {
        isNavigationRequest: () => isNavigation,
        frame: () => frame,
        url: () => url,
      };
      return {
        aborted: false,
        continued: false,
        request: () => request,
        abort: async function () {
          this.aborted = true;
        },
        continue: async function () {
          this.continued = true;
        },
      };
    },
  };
  return page;
}
