// The whole OpenID Connect sign-in, in one browser, against a real provider:
// the first-run wizard chooses "sign in with your own provider", the core
// restarts on the answers, the administrator signs in at the provider and uses
// the API, signs out, and a second person signs in without the administrator's
// rights.
//
// `scripts/check-oidc-providers.sh` starts one provider in a container and a
// fresh core that has never run, and runs this once per provider. Nothing here
// answers for either of them.
//
// What the script puts in the environment:
//
//   PCL_CORE_URL                          the core, on its first run
//   PCL_SETUP_CODE                        the code the core printed
//   PCL_OIDC_PROVIDER                     which sign-in page to fill in
//   PCL_OIDC_ISSUER                       the issuer, as a person types it
//   PCL_OIDC_CLIENT_ID, _CLIENT_SECRET    this server's client at the provider
//   PCL_OIDC_ADMIN_USER, _PASSWORD, _SUB, _NAME, _EMAIL   the administrator
//   PCL_OIDC_VIEWER_USER, _PASSWORD, _SUB, _NAME, _EMAIL  a person with no rights
//
// The server keeps none of the provider's tokens: the callback trades the code
// for an ID token once and sets a session of its own. So there is no refresh
// to check here. A session ends when the person signs out or when it expires.
import type { Browser, BrowserContext, Page, Request } from 'playwright';
import { chromium } from 'playwright';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import { type Person, signInAt } from './providers.ts';
import { userIdOf } from './user-id.ts';

// A provider that serves TLS here does it on a certificate its fixture made,
// which no browser trusts. The core is handed that certificate by the script.
const CONTEXT = {
  viewport: { width: 1280, height: 900 },
  ignoreHTTPSErrors: true,
};

/** The cookie the core sets at the callback. */
const SESSION_COOKIE = 'pcl_session';

const need = (name: string): string => {
  const value = process.env[name];
  if (value === undefined || value === '') {
    throw new Error(`${name} is not set: run \`make oidc-providers-check\``);
  }

  return value;
};

const base = need('PCL_CORE_URL').replace(/\/$/, '');
const setupCode = need('PCL_SETUP_CODE');
const provider = need('PCL_OIDC_PROVIDER');
const issuer = need('PCL_OIDC_ISSUER');
const clientId = need('PCL_OIDC_CLIENT_ID');
const clientSecret = need('PCL_OIDC_CLIENT_SECRET');
const admin: Person = {
  username: need('PCL_OIDC_ADMIN_USER'),
  password: need('PCL_OIDC_ADMIN_PASSWORD'),
  subject: need('PCL_OIDC_ADMIN_SUB'),
  name: need('PCL_OIDC_ADMIN_NAME'),
  email: need('PCL_OIDC_ADMIN_EMAIL'),
};
const viewer: Person = {
  username: need('PCL_OIDC_VIEWER_USER'),
  password: need('PCL_OIDC_VIEWER_PASSWORD'),
  subject: need('PCL_OIDC_VIEWER_SUB'),
  name: need('PCL_OIDC_VIEWER_NAME'),
  email: need('PCL_OIDC_VIEWER_EMAIL'),
};

/** Where the provider sends the browser back to. The wizard fills it in. */
const callback = `${base}/api/v1/auth/callback`;
const issuerOrigin = new URL(issuer).origin;

/** An issuer on the provider's own host that publishes no discovery document. */
const wrongIssuer = `${issuerOrigin}/no-such-issuer`;

const signIn = signInAt(provider);

let browser: Browser;
let context: BrowserContext;
let page: Page;

/**
 * What the sign-in button calls the provider. No name is configured, so it is
 * the issuer's host, the address the person is about to be sent to.
 */
const providerLabel = `Sign in with ${new URL(issuer).hostname}`;

/** The callback the administrator's browser was sent to, for the replay. */
let spentCallback = '';

/** The administrator's session, kept to show it is refused after sign-out. */
let spentSession = '';

let step = 0;
const say = (what: string): void => {
  step += 1;
  process.stdout.write(
    `    ${String(step).padStart(2)}. [${provider}] ${what}\n`,
  );
};

interface Answer {
  readonly status: number;
  readonly body: string;
}

/** Asks the core over the session the page holds, as the page itself does. */
const ask = (on: Page, path: string): Promise<Answer> =>
  on.evaluate(async (where: string) => {
    const answer = await fetch(where, {
      headers: { accept: 'application/json' },
      redirect: 'manual',
    });

    return { status: answer.status, body: await answer.text() };
  }, path);

interface Principal {
  readonly principal: string;
  readonly display_name?: string;
  readonly email?: string;
}

/** Who the core resolves the page's session to. */
const whoIs = async (on: Page): Promise<Principal> => {
  const answer = await ask(on, '/api/v1/principal');
  expect(answer.status, answer.body).toBe(200);

  return JSON.parse(answer.body) as Principal;
};

/** The subject the core resolves the page's session to. */
const principalOf = async (on: Page): Promise<string> =>
  (await whoIs(on)).principal;

/** The statuses the wizard drew for the three steps of a provider test. */
const triedSteps = (on: Page): Promise<(string | undefined)[]> =>
  on
    .locator('[data-part="identity-oidc-try"] li')
    .evaluateAll((items) =>
      items.map((item) => (item as HTMLElement).dataset.status),
    );

const next = (on: Page): Promise<void> =>
  on.getByRole('button', { name: 'Next' }).click();

/**
 * Presses the sign-in button and signs the person in at the provider. It
 * returns the authorization request the core sent the browser to and the
 * callback the provider sent it back to.
 */
const signInThroughProvider = async (
  on: Page,
  person: Person,
): Promise<{ authorization: URL; callback: string }> => {
  const begun = on.waitForResponse(
    (response) => new URL(response.url()).pathname === '/api/v1/auth/login',
  );
  const returned = on.waitForRequest(
    (request: Request) => request.url().startsWith(callback),
    { timeout: 90_000 },
  );

  await on.getByRole('button', { name: providerLabel }).click();

  const location = (await begun).headers().location;
  expect(location, 'the login answered with no Location').toBeDefined();
  const authorization = new URL(location ?? '');

  // `commit` and not `load`: a provider's page may keep a request open for as
  // long as it is shown, and its load event then never fires.
  await on.waitForURL((url) => url.origin === issuerOrigin, {
    timeout: 60_000,
    waitUntil: 'commit',
  });
  await signIn(on, person);

  const back = (await returned).url();
  await on.waitForURL(
    (url) => url.origin === new URL(base).origin && url.pathname === '/',
    {
      timeout: 60_000,
    },
  );
  await on.locator('main').first().waitFor({ timeout: 60_000 });

  return { authorization, callback: back };
};

beforeAll(async () => {
  browser = await chromium.launch();
  context = await browser.newContext(CONTEXT);
  // Playwright waits for ever by default. A step that hangs fails here, with
  // the step's name, before the case's own budget runs out.
  context.setDefaultTimeout(60_000);
  page = await context.newPage();
}, 180_000);

afterAll(async () => {
  await browser.close();
});

describe(`the sign-in at ${provider}`, () => {
  it('walks the wizard to the provider', async () => {
    await page.goto(`${base}/`, { waitUntil: 'domcontentloaded' });
    await page.getByRole('button', { name: 'Start the setup' }).click();
    await page.getByLabel('Setup code').fill(setupCode);
    await next(page);
    await page.getByRole('radio', { name: /^Basic/ }).check();
    await next(page);
    await page.getByRole('radio', { name: /own provider/ }).check();
    say('the wizard offers the sign-in at a provider');
  });

  it('refuses an issuer that publishes no discovery document', async () => {
    await page.getByLabel('Issuer address').fill(wrongIssuer);
    await page.getByLabel('Client ID').fill(clientId);
    await page.getByLabel('Client secret').fill(clientSecret);
    await page.getByLabel(/subject/).fill(admin.subject);
    await page.getByRole('button', { name: 'Test the provider' }).click();

    await expect
      .poll(() => triedSteps(page), { timeout: 30_000 })
      .toEqual(['failed', 'skipped', 'skipped']);
    say(
      'the test of a wrong issuer fails at discovery and tries nothing after it',
    );
  });

  it('tries the real provider: discovery, the client and the redirect', async () => {
    // The redirect address stays empty: the wizard fills in the address this
    // page is on, which is the one the provider has registered.
    expect(await page.getByLabel('Redirect address').inputValue()).toBe('');
    expect(
      await page.getByLabel('Redirect address').getAttribute('placeholder'),
    ).toBe(callback);

    await page.getByLabel('Issuer address').fill(issuer);
    await page.getByRole('button', { name: 'Test the provider' }).click();

    await expect
      .poll(() => triedSteps(page), { timeout: 30_000 })
      .toEqual(['passed', 'passed', 'passed']);
    say(
      'the provider answers, takes the client and takes the redirect address',
    );
  });

  it('refuses a confirm that names a wrong issuer, and restarts on the right one', async () => {
    for (let index = 0; index < 10; index += 1) {
      if (
        (await page
          .getByRole('button', { name: 'Confirm and restart' })
          .count()) > 0
      ) {
        break;
      }
      await next(page);
    }

    // The confirm the wizard sends is sent once more first, with only the
    // issuer changed. The server reads the discovery document before it
    // writes anything, because the restart it asks for could not read it.
    let refused: Answer | undefined;
    await page.route(`${base}/api/v1/setup`, async (route) => {
      const sent = route.request();
      if (sent.method() !== 'POST' || refused !== undefined) {
        await route.continue();
        return;
      }

      const body = JSON.parse(sent.postData() ?? '{}') as {
        configuration: Record<string, unknown>;
      };
      expect(body.configuration['core.auth.issuer']).toBe(issuer);
      expect(body).not.toHaveProperty('account');

      const wrong = await route.fetch({
        postData: JSON.stringify({
          ...body,
          configuration: {
            ...body.configuration,
            'core.auth.issuer': wrongIssuer,
          },
        }),
      });
      refused = { status: wrong.status(), body: await wrong.text() };

      await route.continue();
    });

    await page.getByRole('button', { name: 'Confirm and restart' }).click();

    await expect.poll(() => refused?.status, { timeout: 30_000 }).toBe(422);
    expect(refused?.body).toContain('body.configuration.core.auth.issuer');
    say('the confirm refuses a wrong issuer before it writes anything');

    await page
      .getByRole('heading', { name: 'This server is ready' })
      .waitFor({ timeout: 120_000 });
    await page.unroute(`${base}/api/v1/setup`);
    say('the server restarts on the provider');

    await page.getByRole('button', { name: 'Go to the fleet' }).click();
    await page
      .getByRole('button', { name: providerLabel })
      .waitFor({ timeout: 60_000 });
    expect(await page.locator('input[type="password"]').count()).toBe(0);
    say(`the sign-in page offers "${providerLabel}" and asks for no password`);
  });

  it('signs the administrator in at the provider', async () => {
    const { authorization, callback: back } = await signInThroughProvider(
      page,
      admin,
    );
    spentCallback = back;

    // The request the core sent the browser to: an authorization code flow
    // with PKCE, bound to this login by a state and a nonce.
    const query = authorization.searchParams;
    expect(
      `${authorization.origin}${authorization.pathname}`.startsWith(
        issuerOrigin,
      ),
    ).toBe(true);
    expect(query.get('response_type')).toBe('code');
    expect(query.get('client_id')).toBe(clientId);
    expect(query.get('redirect_uri')).toBe(callback);
    expect(query.get('scope')?.split(' ')).toContain('openid');
    expect(query.get('code_challenge_method')).toBe('S256');
    expect(query.get('code_challenge')).toMatch(/^[\w-]{43}$/);
    expect(query.get('state')).toMatch(/^[\w-]{43}$/);
    expect(query.get('nonce')).toMatch(/^[\w-]{43}$/);
    expect(new URL(back).searchParams.get('state')).toBe(query.get('state'));
    say('the login is a code flow with S256 PKCE, a state and a nonce');

    const cookie = (await context.cookies(base)).find(
      (each) => each.name === SESSION_COOKIE,
    );
    expect(cookie, 'the callback set no session cookie').toBeDefined();
    expect(cookie?.httpOnly).toBe(true);
    expect(cookie?.sameSite).toBe('Lax');
    spentSession = cookie?.value ?? '';
    say("the callback sets an HttpOnly session cookie of the core's own");

    const adminId = userIdOf(issuer, admin.subject);
    if (provider === 'authentik') {
      // authentik's default subject is a 64-character hash, which no user id
      // can be. The fixture keeps that default so this lane signs in through
      // the mapping.
      expect(adminId).toMatch(/^oidc-/);
    }
    expect(await principalOf(page)).toBe(`user:${adminId}`);
    say(
      adminId === admin.subject
        ? `the administrator is user:${adminId}, the subject the provider named`
        : `the administrator is user:${adminId}, the id the provider's subject maps to`,
    );

    const diagnostics = await ask(page, '/api/v1/diagnostics');
    expect(diagnostics.status, diagnostics.body).toBe(200);
    say('the administrator holds the admin role: the diagnostics answer');

    const who = await whoIs(page);
    expect(who.display_name).toBe(admin.name);
    expect(who.email).toBe(admin.email);
    await page.getByRole('button', { name: admin.name }).click();
    await page
      .getByRole('menu')
      .getByText(admin.email)
      .waitFor({ timeout: 30_000 });
    await page.keyboard.press('Escape');
    say(
      `the page names the administrator ${admin.name}, ${admin.email}, as the provider does`,
    );
  });

  it('refuses a callback a second time, and one whose state it never minted', async () => {
    const replayed = await ask(page, spentCallback);
    expect(replayed.status, replayed.body).toBe(401);

    const forged = await ask(
      page,
      `/api/v1/auth/callback?state=${'f'.repeat(43)}&code=forged`,
    );
    expect(forged.status, forged.body).toBe(401);
    say('a spent callback and a forged state are both refused');
  });

  it('signs the administrator out', async () => {
    await page.getByRole('button', { name: admin.name }).click();
    await page.getByRole('menuitem', { name: 'Sign out' }).click();

    await page
      .getByRole('button', { name: providerLabel })
      .waitFor({ timeout: 30_000 });
    expect(await page.locator('input[type="password"]').count()).toBe(0);
    say(
      "signing out returns to the provider's sign-in, not to a password form",
    );

    expect((await ask(page, '/api/v1/principal')).status).toBe(401);

    // The session the browser dropped is refused when somebody presents it,
    // not only forgotten by the browser.
    const presented = await fetch(`${base}/api/v1/principal`, {
      headers: { cookie: `${SESSION_COOKIE}=${spentSession}` },
    });
    expect(presented.status).toBe(401);
    say('the session is revoked on the server, so the old cookie is refused');
  });

  it("signs a second person in without the administrator's rights", async () => {
    const second = await browser.newContext(CONTEXT);
    second.setDefaultTimeout(60_000);
    const other = await second.newPage();
    try {
      await other.goto(`${base}/`, { waitUntil: 'domcontentloaded' });
      await signInThroughProvider(other, viewer);

      const viewerId = userIdOf(issuer, viewer.subject);
      const who = await whoIs(other);
      expect(who.principal).toBe(`user:${viewerId}`);
      expect(who.display_name).toBe(viewer.name);
      say(`the second person is user:${viewerId}, named ${viewer.name}`);

      const diagnostics = await ask(other, '/api/v1/diagnostics');
      expect(diagnostics.status, diagnostics.body).toBe(403);
      say('the second person holds no admin role: the diagnostics refuse');
    } finally {
      await second.close();
    }
  });
});
