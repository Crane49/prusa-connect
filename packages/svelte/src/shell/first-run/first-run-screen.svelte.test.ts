import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import FirstRunScreen from './first-run-screen.svelte';

const MISMATCH = 'The two passwords do not match.';
const TOO_SHORT = 'Use at least eight characters.';
const NO_USERNAME = 'Enter the name this administrator signs in with.';
const NO_CODE = "Enter the setup code from the server's log.";

// A password field carries no ARIA role, so the label is the only handle. The
// anchors keep `Password` from also matching `Confirm password`.
const codeField = () => page.getByLabelText(/^Setup code$/);
const usernameField = () => page.getByLabelText(/^Username$/);
const passwordField = () => page.getByLabelText(/^Password$/);
const confirmField = () => page.getByLabelText(/^Confirm password$/);
const submitButton = () =>
  page.getByRole('button', { name: 'Create administrator' });

const fill = async (values: {
  code?: string;
  username?: string;
  password?: string;
  confirm?: string;
}): Promise<void> => {
  if (values.code !== undefined) await codeField().fill(values.code);
  if (values.username !== undefined)
    await usernameField().fill(values.username);
  if (values.password !== undefined)
    await passwordField().fill(values.password);
  if (values.confirm !== undefined) await confirmField().fill(values.confirm);
};

describe('FirstRunScreen', () => {
  it('names itself and says why it is asking', async () => {
    await render(FirstRunScreen, {
      props: { host: 'printers.local', onCreate: () => undefined },
    });

    await expect
      .element(page.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Welcome');
    await expect
      .element(
        page.getByText(
          'This server has no accounts yet. The one you create now is an administrator.',
        ),
      )
      .toBeVisible();
    await expect.element(page.getByText('printers.local')).toBeVisible();
    await expect.element(submitButton()).toBeVisible();
  });

  it('hands over the name, the password and the setup code', async () => {
    const onCreate = vi.fn();
    await render(FirstRunScreen, { props: { onCreate } });

    await fill({
      code: ' K7QD-3M9X-TP2A ',
      username: 'admin',
      password: 'correcthorse',
      confirm: 'correcthorse',
    });
    await submitButton().click();

    expect(onCreate).toHaveBeenCalledExactlyOnceWith(
      'admin',
      'correcthorse',
      'K7QD-3M9X-TP2A',
    );
  });

  it('says where the setup code is', async () => {
    await render(FirstRunScreen, { props: { onCreate: () => undefined } });

    await expect
      .element(page.getByText(/docker logs for a container/))
      .toBeVisible();
  });

  it('refuses a first run with no setup code', async () => {
    const onCreate = vi.fn();
    await render(FirstRunScreen, { props: { onCreate } });

    await fill({
      username: 'admin',
      password: 'correcthorse',
      confirm: 'correcthorse',
    });
    expect(page.getByText(NO_CODE).query()).toBe(null);

    await submitButton().click();

    await expect.element(page.getByText(NO_CODE)).toBeVisible();
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('draws a wrong setup code in the server own words', async () => {
    await render(FirstRunScreen, {
      props: {
        error: { code: 'core.auth/SETUP_CODE_REFUSED', status: 403 },
        onCreate: () => undefined,
      },
    });

    await expect
      .element(page.getByText('The setup code is missing or wrong'))
      .toBeVisible();
  });

  it('refuses two passwords that differ', async () => {
    const onCreate = vi.fn();
    await render(FirstRunScreen, { props: { onCreate } });

    await fill({
      username: 'admin',
      password: 'correcthorse',
      confirm: 'correcthorsf',
    });
    await submitButton().click();

    await expect.element(page.getByText(MISMATCH)).toBeVisible();
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('keeps quiet about the mismatch until the second field is touched', async () => {
    await render(FirstRunScreen, { props: { onCreate: () => undefined } });

    await fill({ username: 'admin', password: 'correcthorse' });
    expect(page.getByText(MISMATCH).query()).toBe(null);

    await fill({ confirm: 'c' });
    await expect.element(page.getByText(MISMATCH)).toBeVisible();
  });

  it('refuses a password the hasher would take but nobody should', async () => {
    const onCreate = vi.fn();
    await render(FirstRunScreen, { props: { onCreate } });

    await fill({ username: 'admin', password: 'short7c', confirm: 'short7c' });
    expect(page.getByText(TOO_SHORT).query()).toBe(null);

    await submitButton().click();

    await expect.element(page.getByText(TOO_SHORT)).toBeVisible();
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('refuses an administrator with no name', async () => {
    const onCreate = vi.fn();
    await render(FirstRunScreen, { props: { onCreate } });

    await fill({ password: 'correcthorse', confirm: 'correcthorse' });
    expect(page.getByText(NO_USERNAME).query()).toBe(null);

    await submitButton().click();

    await expect.element(page.getByText(NO_USERNAME)).toBeVisible();
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('draws a refusal in the server own words', async () => {
    await render(FirstRunScreen, {
      props: {
        error: { code: 'core.auth/SETUP_CLOSED', status: 409 },
        onCreate: () => undefined,
      },
    });

    await expect
      .element(
        page.getByText('First-run setup has already completed on this server'),
      )
      .toBeVisible();
  });

  it('shuts the button while the decision is being recorded', async () => {
    await render(FirstRunScreen, {
      props: { busy: true, onCreate: () => undefined },
    });

    await expect.element(submitButton()).toBeDisabled();
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(FirstRunScreen, {
      props: { host: 'printers.local', onCreate: () => undefined },
    });

    await expectNoViolations(container);
  });
});
