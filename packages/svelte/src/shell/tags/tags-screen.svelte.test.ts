import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import {
  fakeDevice,
  fakeFleetSource,
  fakeListingStore,
} from '../../test/fakes';
import TagsScreen from './tags-screen.svelte';

const store = fakeListingStore();

const tagged = (id: string, tags: string[]) => ({
  ...fakeDevice(id, `MK4S ${id}`),
  tags,
});

/** One tag on two printers, spelled two ways, and one on a single printer. */
const fleet = () =>
  fakeFleetSource([
    tagged('p0', ['Workshop', 'night shift']),
    tagged('p1', ['workshop']),
    tagged('p2', []),
  ]);

/** The panel bits-ui portals to the end of the document. */
const panel = () => {
  const surface = document.body.querySelector<HTMLElement>(
    '[data-slot="popover-content"]',
  );
  if (surface === null) throw new Error('no panel opened');

  return page.elementLocator(surface);
};

describe('TagsScreen', () => {
  it('names itself and lists one row per tag', async () => {
    const screen = await render(TagsScreen, {
      props: { fleet: fleet(), store },
    });

    await expect
      .element(page.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Tags');
    await expect.element(page.getByText('Workshop')).toBeVisible();
    await expect.element(page.getByText('night shift')).toBeVisible();
    // The head row, and one row for each tag.
    expect(page.getByRole('row').elements()).toHaveLength(3);

    await expectNoViolations(screen.container);
  });

  // The server takes two tags that differ only in case as one tag.
  it('counts the printers behind a tag and opens them', async () => {
    await render(TagsScreen, { props: { fleet: fleet(), store } });

    await page.getByRole('button', { name: 'On 2 printers' }).click();

    expect(panel().getByRole('link').elements()).toHaveLength(2);
    await expect
      .element(panel().getByRole('link', { name: 'MK4S p0' }))
      .toHaveAttribute('href', '/printers/p0');
  });

  it('links to the printers that carry a tag', async () => {
    await render(TagsScreen, {
      props: {
        fleet: fleet(),
        store,
        printersHref: (tag: string) => `/printers?tag=${tag}`,
      },
    });

    await expect
      .element(
        page.getByRole('link', {
          name: 'Show the printers tagged night shift',
        }),
      )
      .toHaveAttribute('href', '/printers?tag=night shift');
  });

  it('offers no way to the printers where the host offers none', async () => {
    await render(TagsScreen, { props: { fleet: fleet(), store } });

    await expect.element(page.getByText('night shift')).toBeVisible();
    expect(
      page.getByRole('link', { name: /Show the printers tagged/ }).query(),
    ).toBeNull();
  });

  it('draws the listing skeleton before the first answer', async () => {
    await render(TagsScreen, {
      props: {
        fleet: fakeFleetSource([], { loading: true, loaded: false }),
        store,
      },
    });

    await expect
      .element(page.getByRole('status', { name: 'Loading tags' }))
      .toBeVisible();
  });

  it('says where a tag is added when no printer carries one', async () => {
    const screen = await render(TagsScreen, {
      props: { fleet: fakeFleetSource([fakeDevice('p0')]), store },
    });

    await expect
      .element(page.getByRole('heading', { name: 'No tags yet' }))
      .toBeVisible();

    await expectNoViolations(screen.container);
  });

  it('draws the error rather than an empty list', async () => {
    await render(TagsScreen, {
      props: {
        fleet: fakeFleetSource([], { error: new Error('the fleet is closed') }),
        store,
      },
    });

    await expect.element(page.getByText('the fleet is closed')).toBeVisible();
    await expect
      .element(page.getByRole('heading', { name: 'No tags yet' }))
      .not.toBeInTheDocument();
  });
});
