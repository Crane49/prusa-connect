import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { AppPairing } from '../../data';
import { expectNoViolations } from '../../test/axe';
import AppPairingScreen from './app-pairing-screen.svelte';
import type { AppPairingScreenProps, AppPairingSource } from './types';

const pairing: AppPairing = {
  user_code: 'BCDF-GHJK',
  app_name: 'Kitchen display',
  scopes: ['read', 'operate'],
  client_address: '192.0.2.41',
  expires_at: new Date(Date.now() + 8 * 60_000).toISOString(),
};

const source = (over: Partial<AppPairingSource> = {}): AppPairingSource => ({
  code: 'BCDF-GHJK',
  pairing,
  loading: false,
  loaded: true,
  error: undefined,
  deciding: false,
  outcome: undefined,
  ...over,
});

const screen = (
  over: Partial<AppPairingScreenProps> = {},
): AppPairingScreenProps => ({
  review: source(),
  linked: true,
  onLookup: () => undefined,
  onApprove: () => undefined,
  onDeny: () => undefined,
  credentialsHref: '/settings/tokens',
  ...over,
});

describe('AppPairingScreen', () => {
  it('asks only to approve the app a link named', async () => {
    await render(AppPairingScreen, { props: screen() });

    await expect
      .element(
        page.getByRole('heading', {
          name: 'Kitchen display wants to connect to Prusa Connect',
        }),
      )
      .toBeVisible();
    await expect
      .element(page.getByText(/start, pause and stop prints/))
      .toBeVisible();
    await expect.element(page.getByText(/192\.0\.2\.41/)).toBeVisible();
    await expect
      .element(page.getByRole('textbox', { name: 'Code' }))
      .not.toBeInTheDocument();
    await expect.element(page.getByRole('radio')).not.toBeInTheDocument();
  });

  // The page sends what the app asked for. A narrower scope is the API's.
  it('approves with exactly the scopes the app asked for', async () => {
    const onApprove = vi.fn();
    await render(AppPairingScreen, { props: screen({ onApprove }) });

    await page.getByRole('button', { name: 'Approve' }).click();

    expect(onApprove).toHaveBeenCalledWith(['read', 'operate']);
  });

  it('denies', async () => {
    const onDeny = vi.fn();
    await render(AppPairingScreen, { props: screen({ onDeny }) });

    await page.getByRole('button', { name: 'Deny' }).click();

    expect(onDeny).toHaveBeenCalledTimes(1);
  });

  it('looks up the code a person types', async () => {
    const onLookup = vi.fn();
    await render(AppPairingScreen, {
      props: screen({
        onLookup,
        linked: false,
        review: source({ code: '', pairing: undefined, loaded: false }),
      }),
    });

    await page.getByRole('textbox', { name: 'Code' }).fill('bcdf-ghjk');
    await page.getByRole('button', { name: 'Find' }).click();

    expect(onLookup).toHaveBeenCalledWith('bcdf-ghjk');
  });

  it('keeps the field beside a pairing found by a typed code', async () => {
    await render(AppPairingScreen, { props: screen({ linked: false }) });

    await expect
      .element(page.getByRole('textbox', { name: 'Code' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Approve' }))
      .toBeVisible();
  });

  it('says it is done once approved, and offers the list', async () => {
    await render(AppPairingScreen, {
      props: screen({ review: source({ outcome: 'approved' }) }),
    });

    await expect
      .element(
        page.getByRole('heading', { name: 'Kitchen display is connected' }),
      )
      .toBeVisible();
    await expect
      .element(page.getByRole('link', { name: 'See your credentials' }))
      .toHaveAttribute('href', '/settings/tokens');
    await expect
      .element(page.getByRole('button', { name: 'Approve' }))
      .not.toBeInTheDocument();
  });

  it('holds both buttons while a decision is on its way', async () => {
    await render(AppPairingScreen, {
      props: screen({ review: source({ deciding: true }) }),
    });

    await expect
      .element(page.getByRole('button', { name: 'Approve' }))
      .toBeDisabled();
    await expect
      .element(page.getByRole('button', { name: 'Deny' }))
      .toBeDisabled();
  });

  // A code the link carried that nothing answers leaves a field to type one.
  it('draws a code nothing answers by its fault', async () => {
    await render(AppPairingScreen, {
      props: screen({
        review: source({
          pairing: undefined,
          error: { code: 'core.auth/APP_PAIRING_MISSING', status: 404 },
        }),
      }),
    });

    await expect
      .element(page.getByText('No waiting pairing has that code'))
      .toBeVisible();
    await expect
      .element(page.getByRole('textbox', { name: 'Code' }))
      .toBeVisible();
  });

  it('has no accessibility violations', async () => {
    const { container } = await render(AppPairingScreen, { props: screen() });

    await expectNoViolations(container);
  });
});
