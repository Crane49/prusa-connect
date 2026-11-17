// What a person does on each provider's own sign-in page.
//
// Everything else in the lane is the same for every provider: the wizard, the
// redirect, the callback and the API. Only the page the provider draws differs,
// so this file is the one place that knows a provider by name.
import type { Page } from 'playwright';

/** A person the provider knows, with the subject it names them by. */
export interface Person {
  readonly username: string;
  readonly password: string;
  readonly subject: string;
  /** What the provider calls the person, and the address it holds. */
  readonly name: string;
  readonly email: string;
}

/** Signs one person in on a provider's page that is already open. */
type SignIn = (page: Page, person: Person) => Promise<void>;

const keycloak: SignIn = async (page, person) => {
  await page.locator('#username').fill(person.username);
  await page.locator('#password').fill(person.password);
  await page.locator('#kc-login').click();
};

// authentik's default authentication flow asks for the name and then for the
// password, each stage on a page of its own. Its pages are web components, and
// Playwright's locators reach into their open shadow roots. The first page
// also carries a password field for a password manager to fill, so the
// password is typed only once the second page has drawn its own button.
const authentik: SignIn = async (page, person) => {
  await page.locator('input[name="uidField"]').fill(person.username);
  await page.getByRole('button', { name: 'Log in' }).click();
  const proceed = page.getByRole('button', { name: 'Continue' });
  await proceed.waitFor();
  await page.locator('input[name="password"]').fill(person.password);
  await proceed.click();
};

// Authelia's portal names its fields by id and not by a label element.
const authelia: SignIn = async (page, person) => {
  await page.locator('#username-textfield').fill(person.username);
  await page.locator('#password-textfield').fill(person.password);
  await page.locator('#sign-in-button').click();
};

const SIGN_IN: Readonly<Record<string, SignIn>> = {
  keycloak,
  authentik,
  authelia,
};

/** The sign-in of one provider, by the name the script passes. */
export const signInAt = (provider: string): SignIn => {
  const signIn = SIGN_IN[provider];
  if (signIn === undefined) {
    throw new Error(
      `no sign-in page is known for ${provider}: add it to test/oidc/providers.ts`,
    );
  }

  return signIn;
};
