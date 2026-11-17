import { expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import PortalPairingWizard from './portal-pairing-wizard.svelte';
import { pairingSource, pendingStatus, phaseSource } from './story-source';

test('the first step explains pairing and starts it', async () => {
  const start = vi.fn(() => Promise.resolve());
  await render(PortalPairingWizard, {
    props: { pairing: pairingSource({ start }) },
  });

  await expect
    .element(page.getByText('Pair this server with the Beta Portal'))
    .toBeInTheDocument();
  await page.getByRole('button', { name: 'Start pairing' }).click();

  expect(start).toHaveBeenCalledOnce();
});

test('a busy pairing blocks a second press', async () => {
  const start = vi.fn(() => Promise.resolve());
  await render(PortalPairingWizard, {
    props: { pairing: pairingSource({ start, busy: 'start' }) },
  });

  const button = page.getByRole('button', { name: 'Start pairing' });
  await expect.element(button).toBeDisabled();
  await button.click({ force: true });

  expect(start).not.toHaveBeenCalled();
});

test('the code step opens the portal with the code filled in, in a new window', async () => {
  const markOpened = vi.fn();
  await render(PortalPairingWizard, {
    props: { pairing: { ...phaseSource('code'), markOpened } },
  });

  await expect.element(page.getByText('WDJB-MJHT')).toBeInTheDocument();
  const link = page.getByRole('link', { name: /Open the Beta Portal/ });
  await expect
    .element(link)
    .toHaveAttribute(
      'href',
      'https://beta.prusa3d.com/device?user_code=WDJB-MJHT',
    );
  await expect.element(link).toHaveAttribute('target', '_blank');
  await expect.element(link).toHaveAttribute('rel', 'noopener noreferrer');
  await expect
    .element(
      page.getByText(/go to https:\/\/beta\.prusa3d\.com\/device and type/),
    )
    .toBeInTheDocument();
  await expect
    .element(page.getByText(/The code is valid for 14 min/))
    .toBeInTheDocument();

  link.element().addEventListener('click', (event) => {
    event.preventDefault();
  });
  await link.click();

  expect(markOpened).toHaveBeenCalledOnce();
});

test('without the filled in address the button opens the plain page', async () => {
  const { user_code, verification_uri } = pendingStatus();
  await render(PortalPairingWizard, {
    props: {
      pairing: pairingSource({
        phase: 'code',
        status: {
          state: 'pending',
          ...(user_code === undefined ? {} : { user_code }),
          ...(verification_uri === undefined ? {} : { verification_uri }),
        },
      }),
    },
  });

  await expect
    .element(page.getByRole('link', { name: /Open the Beta Portal/ }))
    .toHaveAttribute('href', 'https://beta.prusa3d.com/device');
});

test('after the portal opened the wizard waits for the confirmation', async () => {
  await render(PortalPairingWizard, {
    props: { pairing: phaseSource('confirm') },
  });

  await expect
    .element(page.getByText('Waiting for you to confirm on the portal'))
    .toBeInTheDocument();
  await expect
    .element(page.getByRole('link', { name: /Open the portal again/ }))
    .toBeInTheDocument();
  await expect
    .element(
      page.getByRole('listitem').filter({ hasText: 'Confirm on the portal' }),
    )
    .toHaveAttribute('aria-current', 'step');
});

test('a product with team approval adds the step and says who it waits for', async () => {
  await render(PortalPairingWizard, {
    props: { pairing: phaseSource('approval') },
  });

  await expect
    .element(
      page.getByText(
        'Confirmed. Waiting for the Prusa team to approve this server',
      ),
    )
    .toBeInTheDocument();
  await expect
    .element(page.getByRole('listitem').filter({ hasText: 'Team approval' }))
    .toHaveAttribute('aria-current', 'step');
  await expect
    .element(
      page.getByRole('listitem').filter({ hasText: 'Confirm on the portal' }),
    )
    .toHaveAttribute('data-state', 'done');
});

test('a paired server names the account and disconnects after a confirmation', async () => {
  const disconnect = vi.fn(() => Promise.resolve());
  await render(PortalPairingWizard, {
    props: { pairing: { ...phaseSource('paired'), disconnect } },
  });

  await expect
    .element(page.getByText('Paired as Ada Lovelace'))
    .toBeInTheDocument();
  await page.getByRole('button', { name: 'Disconnect' }).click();
  expect(disconnect).not.toHaveBeenCalled();

  const dialog = page.getByRole('alertdialog');
  await expect
    .element(dialog.getByText('Disconnect this server from the Beta Portal?'))
    .toBeInTheDocument();
  await dialog.getByRole('button', { name: 'Disconnect' }).click();

  expect(disconnect).toHaveBeenCalledOnce();
});

test('a pairing with no account name falls back to the product and the device', async () => {
  await render(PortalPairingWizard, {
    props: {
      pairing: pairingSource({
        phase: 'paired',
        status: {
          state: 'paired',
          product: { slug: 'prusa-connect', name: 'Prusa Connect' },
          device: { identifier: 'instance-1' },
        },
      }),
    },
  });

  await expect
    .element(page.getByText('Paired as instance-1 in Prusa Connect'))
    .toBeInTheDocument();
});

test.each([
  ['expired', 'The code expired', 'Start again'],
  ['denied', 'The pairing was declined', 'Start again'],
  ['revoked', 'The Beta Portal no longer accepts this server', 'Pair again'],
] as const)(
  'the %s phase explains itself and starts again',
  async (phase, title, action) => {
    const start = vi.fn(() => Promise.resolve());
    await render(PortalPairingWizard, {
      props: { pairing: { ...phaseSource(phase), start } },
    });

    await expect.element(page.getByText(title)).toBeInTheDocument();
    await page.getByRole('button', { name: action }).click();

    expect(start).toHaveBeenCalledOnce();
  },
);

test('a wrong clock says so, shows the difference and tries again', async () => {
  const retry = vi.fn(() => Promise.resolve());
  await render(PortalPairingWizard, {
    props: { pairing: { ...phaseSource('clock'), retry } },
  });

  await expect
    .element(page.getByText("This server's clock is wrong"))
    .toBeInTheDocument();
  await expect.element(page.getByText(/2h0m0s/)).toBeInTheDocument();
  await expect
    .element(page.getByText('core.resource/BETA_PORTAL_CLOCK_SKEW'))
    .toBeInTheDocument();
  expect(page.getByRole('alert').query()).toBeNull();
  await page.getByRole('button', { name: 'Try again' }).click();

  expect(retry).toHaveBeenCalledOnce();
});

test('an unreachable portal has its own sentence', async () => {
  await render(PortalPairingWizard, {
    props: { pairing: phaseSource('unreachable') },
  });

  await expect
    .element(page.getByText('The Beta Portal cannot be reached'))
    .toBeInTheDocument();
  await expect
    .element(page.getByRole('button', { name: 'Try again' }))
    .toBeInTheDocument();
});

test('the wizard polls while shown and stops when it goes', async () => {
  const stop = vi.fn();
  const watch = vi.fn(() => stop);
  const screen = await render(PortalPairingWizard, {
    props: { pairing: { ...phaseSource('code'), watch } },
  });

  expect(watch).toHaveBeenCalledOnce();
  expect(stop).not.toHaveBeenCalled();

  await screen.unmount();

  expect(stop).toHaveBeenCalledOnce();
});

test('mounting and unmounting leaves no countdown timer behind', async () => {
  const set = vi.spyOn(globalThis, 'setInterval');
  const clear = vi.spyOn(globalThis, 'clearInterval');
  try {
    for (let round = 0; round < 5; round += 1) {
      const screen = await render(PortalPairingWizard, {
        props: { pairing: phaseSource('code') },
      });
      await screen.unmount();
    }

    expect(set.mock.calls.length).toBe(5);
    expect(clear.mock.calls.length).toBe(set.mock.calls.length);
  } finally {
    set.mockRestore();
    clear.mockRestore();
  }
});

test('a phase change is announced through a status region that stays', async () => {
  const screen = await render(PortalPairingWizard, {
    props: { pairing: phaseSource('confirm') },
  });
  const region = page
    .getByRole('status')
    .filter({ hasText: 'Waiting for you' });
  await expect.element(region).toBeInTheDocument();
  const before = region.element();

  await screen.rerender({ pairing: phaseSource('approval') });

  await expect
    .element(
      page.getByRole('status').filter({ hasText: 'Waiting for approval' }),
    )
    .toBeInTheDocument();
  expect(
    page
      .getByRole('status')
      .filter({ hasText: 'Waiting for approval' })
      .element(),
  ).toBe(before);
});

test('a suspended access keeps the pairing and says who can restore it', async () => {
  await render(PortalPairingWizard, {
    props: {
      pairing: pairingSource({
        phase: 'paired',
        status: {
          state: 'paired',
          identity: { subject: 'tester-1', name: 'Ada Lovelace' },
          access_suspended: true,
        },
      }),
    },
  });

  await expect
    .element(page.getByText('The Beta Portal suspended your access'))
    .toBeInTheDocument();
  await expect
    .element(page.getByRole('status'))
    .toHaveTextContent('Access suspended');
  await expect
    .element(page.getByText('Paired as Ada Lovelace'))
    .toBeInTheDocument();
  expect(
    page
      .getByText('This server can download test releases from the Beta Portal.')
      .all(),
  ).toHaveLength(0);
});
