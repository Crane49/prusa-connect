// The module's dashboard cards, drawn the way the dashboard draws them: the
// stub host runs the bundle's own setup, the setup registers each card, and
// each case renders the component that was registered.
import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import type { PartsService } from '../parts';
import { CardParts } from './card-parts.svelte';
import {
  EMPTY_OVERVIEW,
  MOUNT,
  NAMESPACE,
  refusal,
  type Route,
  stubHost,
} from '../test/host';

const HOOK_ROW = {
  id: 5,
  folder: 1,
  name: 'Bike hook',
  summary: 'Holds a bicycle by its front wheel',
  tags: ['bike'],
  versions: 3,
  comments: 0,
  latest: { number: 3, files: 1, bytes: 4096, created: '2026-10-05T09:00:00Z' },
  changed: '2026-10-05T09:00:00Z',
  changed_by: 'user:demo',
};

const CLIP_ROW = { ...HOOK_ROW, id: 7, name: 'Cable clip', latest: undefined };

const HOOK = {
  v: 1,
  id: 5,
  folder: 1,
  name: 'Bike hook',
  summary: 'Holds a bicycle by its front wheel',
  description: '',
  tags: ['bike'],
  cover: '',
  last_version: 3,
  created: '2026-10-05T09:00:00Z',
  created_by: 'user:demo',
  changed: '2026-10-05T09:00:00Z',
  changed_by: 'user:demo',
  revision: 1,
  versions: [],
  comments: [],
  dir: 'parts/5',
  storage: 'local',
};

const OVERVIEW = {
  ...EMPTY_OVERVIEW,
  folders: [
    {
      v: 1,
      id: 1,
      parent: 0,
      name: 'Garage',
      parts: 2,
      folders: 0,
      created: '',
      changed: '',
      changed_by: 'user:demo',
    },
  ],
  tags: [{ tag: 'bike', parts: 1 }],
};

const SIZE = { w: 6, h: 5 };

/** The module set up over a stub host, and its registered cards. */
const started = async (routes: Readonly<Record<string, Route>> = {}) => {
  const stub = stubHost({
    routes: {
      [`GET ${MOUNT}/overview`]: { status: 200, body: OVERVIEW },
      ...routes,
    },
  });
  await setup(stub.host);

  return stub;
};

const listing = (
  rows: readonly (typeof HOOK_ROW | typeof CLIP_ROW)[],
  matched = rows.length,
): Route => ({
  status: 200,
  body: { parts: rows, matched, held: matched, principal: 'user:demo' },
});

describe('the parts folder card', () => {
  it('shows the newest parts of its folder, each a link to its page', async () => {
    const stub = await started({
      [`GET ${MOUNT}/parts`]: listing([HOOK_ROW, CLIP_ROW], 9),
    });
    const card = stub.cards.folder;
    if (card === undefined)
      throw new Error('the module registered no folder card');

    await render(card.component, {
      props: {
        config: { folder: 1, deep: false, tag: '', order: 'changed', limit: 2 },
        title: '',
        size: SIZE,
        editing: false,
      },
    });

    await expect
      .element(page.getByRole('heading', { name: 'Garage' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('link', { name: /Bike hook/ }))
      .toHaveAttribute('href', `/m/${NAMESPACE}/parts?part=5`);
    await expect.element(page.getByText('Version 3')).toBeVisible();
    await expect
      .element(page.getByText('7 more parts in the catalogue'))
      .toBeVisible();

    const [call] = stub.made(`GET ${MOUNT}/parts`);
    expect(new URLSearchParams(call?.search)).toEqual(
      new URLSearchParams('folder=1&deep=false&order=changed&limit=2'),
    );
  });

  it('links nothing while the dashboard is being changed', async () => {
    const stub = await started({ [`GET ${MOUNT}/parts`]: listing([HOOK_ROW]) });

    await render(stub.cards.folder!.component, {
      props: {
        config: { folder: 1, deep: false, tag: '', order: 'changed', limit: 8 },
        title: 'Spares',
        size: SIZE,
        editing: true,
      },
    });

    await expect.element(page.getByText('Bike hook')).toBeVisible();
    await expect.element(page.getByRole('link')).not.toBeInTheDocument();
    await expect
      .element(page.getByRole('heading', { name: 'Spares' }))
      .toBeVisible();
  });

  it('says so when the folder holds no part', async () => {
    const stub = await started();

    await render(stub.cards.folder!.component, {
      props: {
        config: { folder: 0, deep: true, tag: '', order: 'name', limit: 8 },
        title: '',
        size: SIZE,
        editing: false,
      },
    });

    // As the host's own cards say it: a heading, and what to do about it.
    await expect
      .element(
        page.getByRole('heading', {
          level: 3,
          name: 'No part is in this folder.',
        }),
      )
      .toBeVisible();
    await expect
      .element(
        page.getByText(
          'Add parts to it on the Parts page, or pick another folder in the settings of this card.',
        ),
      )
      .toBeVisible();
  });

  it('takes a heading of only spaces as no heading, as the host does', async () => {
    const stub = await started();

    await render(stub.cards.folder!.component, {
      props: {
        config: { folder: 0, deep: true, tag: '', order: 'name', limit: 8 },
        title: '   ',
        size: SIZE,
        editing: false,
      },
    });

    await expect
      .element(page.getByRole('heading', { level: 2, name: 'Catalogue' }))
      .toBeVisible();
  });

  it('hands back a setting changed in its own editor', async () => {
    const stub = await started();
    const changes: unknown[] = [];
    const config = {
      folder: 1,
      deep: false,
      tag: '',
      order: 'changed',
      limit: 8,
    };

    await render(stub.cards.folder!.editor!, {
      props: {
        config,
        onchange: (next: unknown) => {
          changes.push(next);
        },
      },
    });
    await userEvent.click(
      page.getByRole('switch', { name: 'Include the folders inside it' }),
    );

    expect(changes).toEqual([{ ...config, deep: true }]);
  });
});

describe('the chosen parts card', () => {
  it('shows the chosen parts in their order, and counts one that is gone', async () => {
    const stub = await started({
      [`GET ${MOUNT}/parts/5`]: { status: 200, body: { part: HOOK } },
      [`GET ${MOUNT}/parts/99`]: refusal(404, 'parts/PART_NOT_FOUND', 'gone'),
    });

    await render(stub.cards.chosen!.component, {
      props: {
        config: { parts: '5,99' },
        title: '',
        size: SIZE,
        editing: false,
      },
    });

    await expect
      .element(page.getByRole('heading', { name: 'Chosen parts' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('link', { name: /Bike hook/ }))
      .toBeVisible();
    await expect
      .element(page.getByText('1 chosen part is no longer in the catalogue'))
      .toBeVisible();
  });

  it('says what to do when no part is chosen yet', async () => {
    const stub = await started();

    await render(stub.cards.chosen!.component, {
      props: { config: { parts: '' }, title: '', size: SIZE, editing: false },
    });

    await expect
      .element(
        page.getByRole('heading', { level: 3, name: 'No part is chosen.' }),
      )
      .toBeVisible();
    await expect
      .element(page.getByText('Choose parts in the settings of this card.'))
      .toBeVisible();
  });

  it('adds a part found in its editor', async () => {
    const stub = await started({ [`GET ${MOUNT}/parts`]: listing([HOOK_ROW]) });
    const changes: unknown[] = [];

    await render(stub.cards.chosen!.editor!, {
      props: {
        config: { parts: '' },
        onchange: (next: unknown) => {
          changes.push(next);
        },
      },
    });
    await userEvent.click(page.getByRole('button', { name: 'Add Bike hook' }));

    expect(changes).toEqual([{ parts: '5' }]);
  });
});

describe('the parts a card reads', () => {
  it('drops a read the card left, and counts no part as gone', async () => {
    // Each read waits until it is aborted, as a request of a card that left.
    const waiting = {
      part: (_id: number, signal?: AbortSignal) =>
        new Promise((_, reject) => {
          signal?.addEventListener('abort', () => {
            reject(new DOMException('aborted', 'AbortError'));
          });
        }),
    } as unknown as PartsService;
    const parts = new CardParts(waiting);

    const read = parts.readChosen([5, 7]);
    parts.stop();
    await read;

    expect(parts.missing).toBe(0);
    expect(parts.fault).toBeUndefined();
  });

  it('says so when a search in the editor finds nothing', async () => {
    const stub = await started({ [`GET ${MOUNT}/parts`]: listing([]) });

    await render(stub.cards.chosen!.editor!, {
      props: { config: { parts: '' }, onchange: () => undefined },
    });
    await userEvent.fill(page.getByRole('searchbox'), 'nothing like it');

    await expect.element(page.getByText('No part matches')).toBeVisible();
  });
});
