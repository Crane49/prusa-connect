import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import SignInScreen from './sign-in-screen.svelte';

const type = async (label: string, text: string): Promise<void> => {
  await userEvent.click(page.getByLabelText(label));
  await userEvent.keyboard(text);
};

describe('SignInScreen', () => {
  it('names itself and offers the one act', async () => {
    await render(SignInScreen, { props: { onSignIn: () => undefined } });

    await expect
      .element(page.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Sign in');
    await expect
      .element(page.getByRole('button', { name: 'Sign in' }))
      .toBeVisible();
  });

  it('hands the credential over exactly as it was typed', async () => {
    const onSignIn = vi.fn();
    await render(SignInScreen, { props: { onSignIn } });

    await type('Username', 'maker');
    await type('Password', 'hunter2');
    await page.getByRole('button', { name: 'Sign in' }).click();

    expect(onSignIn).toHaveBeenCalledWith('maker', 'hunter2');
  });

  it('submits on Enter from inside a field', async () => {
    const onSignIn = vi.fn();
    await render(SignInScreen, { props: { onSignIn } });

    await type('Username', 'maker');
    await type('Password', 'hunter2{Enter}');

    expect(onSignIn).toHaveBeenCalledWith('maker', 'hunter2');
  });

  it('says which field is missing rather than sending nothing', async () => {
    const onSignIn = vi.fn();
    await render(SignInScreen, { props: { onSignIn } });

    await page.getByRole('button', { name: 'Sign in' }).click();

    await expect
      .element(page.getByText('Enter the name this account was created with.'))
      .toBeVisible();
    await expect
      .element(page.getByText('Enter the password for this account.'))
      .toBeVisible();
    expect(onSignIn).not.toHaveBeenCalled();
  });

  it('holds the complaint back until a send was attempted', async () => {
    await render(SignInScreen, { props: { onSignIn: () => undefined } });

    await type('Username', 'maker');

    expect(page.getByText('Enter the password for this account.').query()).toBe(
      null,
    );
  });

  it('says the session expired only when the server refused the old one', async () => {
    await render(SignInScreen, {
      props: { ended: 'refused', onSignIn: () => undefined },
    });

    await expect.element(page.getByText('Your session expired')).toBeVisible();
  });

  it('says nothing about an expiry on a first sign-in', async () => {
    await render(SignInScreen, {
      props: { ended: 'never', onSignIn: () => undefined },
    });

    await expect.element(page.getByRole('heading', { level: 1 })).toBeVisible();
    expect(page.getByText('Your session expired').query()).toBe(null);
  });

  it('draws the refusal in the words the core registered for it', async () => {
    await render(SignInScreen, {
      props: {
        error: { code: 'core.auth/LOGIN_REFUSED', status: 401 },
        onSignIn: () => undefined,
      },
    });

    await expect
      .element(page.getByText('The username or password is wrong'))
      .toBeVisible();
  });

  it('refuses a second attempt while the first is in flight', async () => {
    const onSignIn = vi.fn();
    await render(SignInScreen, { props: { busy: true, onSignIn } });

    await expect
      .element(page.getByRole('button', { name: 'Sign in' }))
      .toBeDisabled();

    // A disabled submit button is what stops implicit submission, so Enter
    // in a filled form is the assertion that the `disabled` is still there.
    await type('Username', 'maker');
    await type('Password', 'hunter2{Enter}');

    expect(onSignIn).not.toHaveBeenCalled();
  });

  it('draws the host it is signing in to', async () => {
    await render(SignInScreen, {
      props: { host: 'printer.local', onSignIn: () => undefined },
    });

    await expect.element(page.getByText('printer.local')).toBeVisible();
  });

  it('offers one provider button and no password form when the server redirects', async () => {
    const onRedirect = vi.fn();
    await render(SignInScreen, {
      props: {
        redirectProvider: 'oidc',
        onRedirect,
        onSignIn: () => undefined,
      },
    });

    expect(page.getByLabelText('Password').query()).toBe(null);
    await page.getByRole('button', { name: 'Sign in with oidc' }).click();

    expect(onRedirect).toHaveBeenCalledOnce();
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(SignInScreen, {
      props: {
        host: 'printer.local',
        ended: 'refused',
        error: { code: 'core.auth/LOGIN_REFUSED', status: 401 },
        onSignIn: () => undefined,
      },
    });

    await expectNoViolations(container);
  });

  it('says why the last sign-in at the provider did not finish', async () => {
    await render(SignInScreen, {
      props: {
        redirectProvider: 'auth.example.com',
        failure: 'id_token',
        onRedirect: () => undefined,
        onSignIn: () => undefined,
      },
    });

    await expect
      .element(page.getByText('The sign-in did not finish'))
      .toBeVisible();
    await expect
      .element(page.getByText(/could not accept what the provider sent/))
      .toBeVisible();
  });
});
