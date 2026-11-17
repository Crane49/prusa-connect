// The summary names the settings the operator's file keeps, so a person
// knows before the confirm what the wizard leaves out, and what stops it.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { SetupConfiguration, SetupKey } from '../../data';
import { SetupDraft } from './setup-draft.svelte';
import StepSummary from './step-summary.svelte';
import { addWizardTexts } from './texts';

addWizardTexts();

const issuer: SetupKey = {
  path: 'core.auth.issuer',
  group: 'auth',
  level: 'advanced',
  kind: 'text',
  default: null,
  choices: [],
  doc: '',
  secret: false,
  locked: true,
  value: 'https://id.example.com',
  reference: 'config/#key-core.auth.issuer',
};

const contract: SetupConfiguration = {
  config_file: { path: '/srv/config/config.yaml', exists: true },
  setup_file: { path: '/srv/config/config.d/setup.yaml', exists: false },
  keys: [issuer],
  storage: { root: '/srv/storage', declared: false },
  hosts: [],
  printers: true,
  printers_tls: false,
};

const props = (kept: SetupKey[], blocked: ('identity' | 'encryption')[]) => ({
  draft: new SetupDraft(null, null),
  contract,
  error: undefined,
  failure: undefined,
  moves: undefined,
  printerAddress: undefined,
  webOff: false,
  kept,
  blocked,
  onedit: vi.fn(),
});

describe('StepSummary', () => {
  it('names each setting the file keeps, with the value it keeps', async () => {
    await render(StepSummary, { props: props([issuer], []) });

    const notice = page.getByRole('alert').filter({
      hasText: 'Settings from your configuration file',
    });
    await expect.element(notice).toBeVisible();
    await expect
      .element(
        notice
          .getByRole('listitem')
          .getByText('Sign-in provider (OpenID Connect)', { exact: false }),
      )
      .toBeVisible();
    await expect
      .element(notice.getByRole('listitem').getByText('https://id.example.com'))
      .toBeVisible();
    await expect
      .element(notice.getByText(/the wizard cannot finish/u))
      .not.toBeInTheDocument();
  });

  it('says how to settle a group the file and the answers set apart', async () => {
    await render(StepSummary, { props: props([issuer], ['identity']) });

    await expect
      .element(page.getByText(/sign-in provider settings only work together/u))
      .toBeVisible();
  });

  it('shows the printers encrypted when the file gives their listener a certificate', async () => {
    await render(StepSummary, {
      props: {
        ...props([], []),
        contract: { ...contract, printers: false, printers_tls: true },
      },
    });

    const encryption = page
      .getByText('Encryption', { exact: true })
      .element().parentElement;
    expect(encryption?.querySelector('dd')?.textContent.trim()).toBe(
      'Printers',
    );
  });

  it('says nothing about the file when it keeps nothing', async () => {
    await render(StepSummary, { props: props([], []) });

    await expect
      .element(page.getByText('Settings from your configuration file'))
      .not.toBeInTheDocument();
  });
});
