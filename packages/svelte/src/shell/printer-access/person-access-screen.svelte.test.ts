import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { FleetListingQuery } from '../../data';
import { expectNoViolations } from '../../test/axe';
import { fakeGrants, fakePrinters, people, SUBJECT } from './access.fixtures';
import PersonAccessScreen from './person-access-screen.svelte';

const [, jana, workshop, intern] = people;
if (jana === undefined || workshop === undefined || intern === undefined) {
  throw new Error('the people fixture lost a row');
}

describe('PersonAccessScreen', () => {
  it('lists the page of printers the server answered, with the total behind it', async () => {
    await render(PersonAccessScreen, {
      props: {
        account: workshop,
        accountsLoaded: true,
        printers: fakePrinters(),
        grants: fakeGrants({}),
        self: SUBJECT,
        peopleHref: '/settings/people',
      },
    });

    await expect
      .element(page.getByRole('heading', { name: 'Printer access' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('switch', { name: 'Access for CoreOne DEMO-01' }))
      .toBeChecked();
    await expect.element(page.getByText(/400/).first()).toBeVisible();
  });

  it('sends the search to the server rather than filtering a page', async () => {
    const asked: Partial<FleetListingQuery>[] = [];
    await render(PersonAccessScreen, {
      props: {
        account: intern,
        accountsLoaded: true,
        printers: fakePrinters({
          setQuery: (next) => {
            asked.push(next);
          },
        }),
        grants: fakeGrants({}),
        self: SUBJECT,
        peopleHref: '/settings/people',
      },
    });

    await page.getByRole('searchbox', { name: 'Search printers' }).fill('XL');

    expect(asked.at(-1)).toEqual({ search: 'XL' });
  });

  it('marks a printer grant the organization role outranks', async () => {
    await render(PersonAccessScreen, {
      props: {
        account: workshop,
        accountsLoaded: true,
        printers: fakePrinters(),
        grants: fakeGrants({ 'user:workshop': ['viewer'] }),
        self: SUBJECT,
        peopleHref: '/settings/people',
      },
    });

    await expect
      .element(
        page
          .getByText('No effect while the organization role is Operator')
          .first(),
      )
      .toBeVisible();
  });

  it('says once that an organization administrator reaches every printer', async () => {
    await render(PersonAccessScreen, {
      props: {
        account: jana,
        accountsLoaded: true,
        printers: fakePrinters(),
        grants: fakeGrants({}),
        self: SUBJECT,
        peopleHref: '/settings/people',
      },
    });

    await expect
      .element(
        page.getByText(
          'Jana Dvořáková is an organization administrator and reaches every printer.',
        ),
      )
      .toBeVisible();
    expect(page.getByRole('switch').query()).toBe(null);
  });

  it('says there is no such person once the accounts were read', async () => {
    await render(PersonAccessScreen, {
      props: {
        account: undefined,
        accountsLoaded: true,
        printers: fakePrinters(),
        grants: fakeGrants({}),
        self: SUBJECT,
        peopleHref: '/settings/people',
      },
    });

    await expect
      .element(page.getByRole('heading', { name: 'No such person' }))
      .toBeVisible();
  });

  it('passes axe', async () => {
    const { container } = await render(PersonAccessScreen, {
      props: {
        account: intern,
        accountsLoaded: true,
        printers: fakePrinters(),
        grants: fakeGrants({ 'user:intern': ['operator'] }),
        self: SUBJECT,
        peopleHref: '/settings/people',
      },
    });

    await expect
      .element(page.getByRole('switch', { name: 'Access for XL DEMO-03' }))
      .toBeVisible();
    await expectNoViolations(container);
  });
});
