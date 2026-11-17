import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import { fakeListingStore } from '../../test/fakes';
import {
  fakeGrants,
  fakePeople,
  grantsOnPrinter,
  PRINTER,
  SUBJECT,
} from './access.fixtures';
import PrinterAccessCard from './printer-access-card.svelte';

const ADMINISTER = ['observe', 'operate', 'configure', 'administer'];

const card = (grants = fakeGrants(grantsOnPrinter)) =>
  render(PrinterAccessCard, {
    props: {
      deviceId: PRINTER,
      callerActions: ADMINISTER,
      people: fakePeople(),
      grants,
      self: SUBJECT,
      store: fakeListingStore(),
    },
  });

describe('PrinterAccessCard', () => {
  it('locks an organization administrator with the reason, not a dead switch', async () => {
    await card();

    await expect
      .element(page.getByText('jana@printfarm.example'))
      .toBeVisible();
    expect(
      page.getByRole('switch', { name: 'Access for Jana Dvořáková' }).query(),
    ).toBe(null);
    await expect
      .element(
        page
          .getByText(/Organization administrators reach every printer/)
          .first(),
      )
      .toBeVisible();
  });

  it('draws a blocked person off, with the role select waiting', async () => {
    await card();

    const access = page.getByRole('switch', {
      name: 'Access for Workshop shared account',
    });
    await expect.element(access).not.toBeChecked();
    await expect
      .element(
        page.getByRole('combobox', {
          name: 'Role for Workshop shared account',
        }),
      )
      .toBeDisabled();
  });

  it('asks for a block when access is turned off', async () => {
    const grants = fakeGrants(grantsOnPrinter);
    await card(grants);

    await page
      .getByRole('switch', { name: 'Access for intern@printfarm.example' })
      .click();

    expect(grants.asked).toEqual([`${PRINTER} user:intern off`]);
  });

  it('holds both controls while a change is in flight', async () => {
    await card(
      fakeGrants(grantsOnPrinter, {
        busy: (_device, subject) => subject === 'user:intern',
      }),
    );

    await expect
      .element(
        page.getByRole('switch', {
          name: 'Access for intern@printfarm.example',
        }),
      )
      .toBeDisabled();
    await expect
      .element(
        page.getByRole('combobox', {
          name: 'Role for intern@printfarm.example',
        }),
      )
      .toBeDisabled();
  });

  it('narrows the people by the search', async () => {
    await card();

    await page.getByRole('searchbox', { name: 'Search people' }).fill('intern');

    expect(page.getByText('jana@printfarm.example').query()).toBe(null);
    await expect
      .element(page.getByText('intern@printfarm.example'))
      .toBeVisible();
  });

  it('draws no card for a caller who may not list the people', async () => {
    await render(PrinterAccessCard, {
      props: {
        deviceId: PRINTER,
        callerActions: ADMINISTER,
        people: { ...fakePeople([]), loaded: false, error: new Error('403') },
        grants: fakeGrants({}),
        self: SUBJECT,
        store: fakeListingStore(),
      },
    });

    expect(page.getByRole('heading', { name: 'Access' }).query()).toBe(null);
  });

  // The test viewport is a phone's, 414px. Without the wrap the select and
  // the switch squeezed a username to four letters and an ellipsis.
  it('keeps the name readable on a phone by moving the controls under it', async () => {
    await card();

    const name = page.getByText('workshop@printfarm.example').element();
    expect(name.getBoundingClientRect().width).toBeGreaterThanOrEqual(150);
    expect(name.scrollWidth).toBeLessThanOrEqual(name.clientWidth);
  });

  it('passes axe', async () => {
    const { container } = await card();

    await expectNoViolations(container);
  });
});
