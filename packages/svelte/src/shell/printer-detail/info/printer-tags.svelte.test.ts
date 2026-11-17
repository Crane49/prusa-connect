import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../../test/axe';
import PrinterTags from './printer-tags.svelte';
import type { PrinterAdministration } from './types';

type Tags = readonly string[];

// A store that applies each change to the list the last change left, starting
// from the record. The real store applies it to the list the server holds.
const store = (
  record: Tags = [],
): PrinterAdministration & { written: Tags[] } => {
  let latest: Tags | undefined;
  const written: Tags[] = [];

  return {
    written,
    pending: () => false,
    error: undefined,
    failed: undefined,
    tags: (shown) => latest ?? shown,
    editTags: (change) => {
      latest = change(latest ?? record);
      written.push(latest);

      return Promise.resolve(true);
    },
    rename: () => Promise.resolve(true),
    retire: () => Promise.resolve(true),
  };
};

const record = ['assembly', 'workshop 2'];

describe('PrinterTags', () => {
  it('draws each tag as the operator typed it', async () => {
    await render(PrinterTags, {
      props: { tags: ['PETG only', 'night shift'] },
    });

    await expect.element(page.getByText('PETG only')).toBeVisible();
    await expect.element(page.getByText('night shift')).toBeVisible();
  });

  it('says so when the printer carries no tag', async () => {
    await render(PrinterTags, { props: { tags: [] } });

    await expect
      .element(page.getByText('Nobody has tagged this printer.'))
      .toBeVisible();
  });

  // The core replaces the whole tag list on a write.
  it('takes a tag off by writing the list without it', async () => {
    const administration = store(record);
    await render(PrinterTags, { props: { tags: record, administration } });

    await page.getByRole('button', { name: 'Remove the tag assembly' }).click();

    expect(administration.written).toEqual([['workshop 2']]);
  });

  // The record the component was drawn from is stale after the first edit.
  // A second edit built on it would put the removed tag back.
  it('builds a second quick edit on the first, not on the record', async () => {
    const administration = store(record);
    await render(PrinterTags, { props: { tags: record, administration } });

    await page.getByRole('button', { name: 'Remove the tag assembly' }).click();
    await page
      .getByRole('button', { name: 'Remove the tag workshop 2' })
      .click();

    expect(administration.written).toEqual([['workshop 2'], []]);
  });

  it('adds a typed tag at the end of the list, trimmed', async () => {
    const administration = store(record);
    await render(PrinterTags, { props: { tags: record, administration } });

    await page.getByRole('button', { name: 'Add a tag' }).click();
    await page.getByRole('textbox', { name: 'Tag' }).fill('  night shift ');
    await page.getByRole('button', { name: 'Add', exact: true }).click();

    expect(administration.written).toEqual([
      ['assembly', 'workshop 2', 'night shift'],
    ]);
  });

  // Another person added the same tag in another spelling before this write.
  // Adding it again would be refused as a duplicate, so the list is kept.
  it('adds nothing when the server already holds the tag in another spelling', async () => {
    const administration = store(['workshop', 'NIGHT SHIFT']);
    await render(PrinterTags, {
      props: { tags: ['workshop'], administration },
    });

    await page.getByRole('button', { name: 'Add a tag' }).click();
    await page.getByRole('textbox', { name: 'Tag' }).fill('night shift');
    await page.getByRole('button', { name: 'Add', exact: true }).click();

    expect(administration.written).toEqual([['workshop', 'NIGHT SHIFT']]);
  });

  // The server may hold the tag in another spelling than the page drew.
  it('removes a tag the server holds in another spelling', async () => {
    const administration = store(['WORKSHOP', 'night shift']);
    await render(PrinterTags, {
      props: { tags: ['Workshop', 'night shift'], administration },
    });

    await page.getByRole('button', { name: 'Remove the tag Workshop' }).click();

    expect(administration.written).toEqual([['night shift']]);
  });

  it('adds a typed tag with the Enter key', async () => {
    const administration = store([]);
    await render(PrinterTags, { props: { tags: [], administration } });

    await page.getByRole('button', { name: 'Add a tag' }).click();
    await page.getByRole('textbox', { name: 'Tag' }).fill('workshop');
    await userEvent.keyboard('{Enter}');

    expect(administration.written).toEqual([['workshop']]);
  });

  // The server decides whether a tag is allowed, so the dialog sends what was
  // typed. A field with only spaces in it is not sent at all.
  it('sends nothing for a tag of only spaces', async () => {
    const administration = store(record);
    await render(PrinterTags, { props: { tags: record, administration } });

    await page.getByRole('button', { name: 'Add a tag' }).click();
    await page.getByRole('textbox', { name: 'Tag' }).fill('   ');

    await expect
      .element(page.getByRole('button', { name: 'Add', exact: true }))
      .toBeDisabled();
    expect(administration.written).toEqual([]);
  });

  // Two tags that differ only in case are one tag, as the server decides.
  it('says so when the printer already carries the typed tag', async () => {
    const administration = store(record);
    await render(PrinterTags, { props: { tags: record, administration } });

    await page.getByRole('button', { name: 'Add a tag' }).click();
    await page.getByRole('textbox', { name: 'Tag' }).fill('ASSEMBLY');

    await expect
      .element(page.getByText('This printer already carries that tag.'))
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Add', exact: true }))
      .toBeDisabled();
    expect(administration.written).toEqual([]);
  });

  // The remove button is the library's own, and it is at least 24 pixels
  // square, so a finger can hit it.
  it('draws each remove control at least 24 pixels square', async () => {
    await render(PrinterTags, {
      props: { tags: record, administration: store(record) },
    });

    const remove = page
      .getByRole('button', { name: 'Remove the tag assembly' })
      .element()
      .getBoundingClientRect();
    expect(remove.width).toBeGreaterThanOrEqual(24);
    expect(remove.height).toBeGreaterThanOrEqual(24);
  });

  it('offers no control without a store to write through', async () => {
    const { container } = await render(PrinterTags, {
      props: { tags: record },
    });

    expect(container.querySelectorAll('button')).toHaveLength(0);
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(PrinterTags, {
      props: { tags: record, administration: store(record) },
    });

    await expectNoViolations(container);
  });
});
