// The Extensions listing as rows: the join of what is offered and what is
// installed, the search, the three dimensions and their counts, and the order.
import { describe, expect, it } from 'vitest';

import type { AnnouncedPage, ResourceInstance } from '@prusa/connect-ui/data';

import type { ModuleOffer } from '../stores/module-catalogue.svelte';
import {
  compareExtensions,
  extensionFacetCounts,
  extensionRowsOf,
  filterExtensions,
  hidesCommunity,
  NO_EXTENSIONS_SELECTED,
  searchTerm,
  SOURCES,
  STATES,
  TRUSTS,
} from './rows';

const offer = (over: Partial<ModuleOffer> = {}): ModuleOffer => ({
  module: 'github.com/prusa3d/parts',
  name: 'parts',
  title: 'Parts',
  description: 'A private catalogue of parts.',
  version: '0.2.0',
  source: 'http://localhost:4455/download',
  author: 'Prusa Research',
  installed: false,
  process: true,
  requests_known: true,
  namespace: 'com.github.aux.prusa3d.parts',
  origin: 'marketplace',
  ...over,
});

const SPOOLS = offer({
  module: 'github.com/acme/spools',
  name: 'spools',
  title: 'Spools',
  description: 'Counts the filament.',
  author: 'Acme',
  namespace: 'com.github.aux.acme.spools',
  origin: 'local',
  installed: true,
  installed_version: '0.2.0',
});

const installed = (namespace: string, name: string): ResourceInstance =>
  ({
    kind: 'sink',
    name,
    namespace,
    module: { version: '1.0.0', summary: 'Installed by hand.', author: 'me' },
  }) as ResourceInstance;

const PAGES: AnnouncedPage[] = [
  {
    namespace: 'com.github.aux.prusa3d.parts',
    path: 'parts',
    title: 'Parts',
    hidden: false,
    href: '/m/com.github.aux.prusa3d.parts/parts',
  },
  {
    namespace: 'com.github.aux.prusa3d.parts',
    path: 'hidden',
    title: 'Hidden',
    hidden: true,
    href: '/m/com.github.aux.prusa3d.parts/hidden',
  },
];

describe('the rows of the Extensions listing', () => {
  it('joins an installed offer to its instance', () => {
    const instance = {
      ...installed('com.github.aux.prusa3d.parts', 'parts'),
      kind: 'asset',
    };
    const [parts] = extensionRowsOf(
      [
        offer({
          installed: true,
          installed_version: '0.2.0',
          instance: 'asset:parts',
        }),
      ],
      [instance],
      PAGES,
    );

    expect(parts?.instance).toBe(instance);
  });

  it('draws every offered row, with the pages its module adds', () => {
    const [parts] = extensionRowsOf([offer()], [], PAGES);

    expect(parts).toMatchObject({
      key: 'github.com/prusa3d/parts',
      title: 'Parts',
      source: 'marketplace',
      state: 'available',
      version: '0.2.0',
      author: 'Prusa Research',
    });
    // A hidden page keeps its route and is not offered.
    expect(parts?.pages).toEqual([
      { title: 'Parts', href: '/m/com.github.aux.prusa3d.parts/parts' },
    ]);
  });

  it('lists an installed module no index offers once, and an offered one once', () => {
    const rows = extensionRowsOf(
      [offer()],
      [
        installed('com.github.aux.prusa3d.parts', 'parts'),
        installed('com.github.aux.me.by-hand', 'by-hand'),
      ],
      [],
    );

    expect(rows.map((row) => row.key)).toEqual([
      'github.com/prusa3d/parts',
      'com.github.aux.me.by-hand',
    ]);
    expect(rows[1]).toMatchObject({
      title: 'by-hand',
      source: 'unlisted',
      state: 'installed',
      version: '1.0.0',
      summary: 'Installed by hand.',
    });
    // No index offers it, so there is no release to install or update to.
    expect(rows[1]?.offer).toBeUndefined();
  });

  it('leaves out a resource that is no module', () => {
    const storage = { kind: 'storage', name: 'local' } as ResourceInstance;

    expect(extensionRowsOf([], [storage], [])).toEqual([]);
  });
});

describe('narrowing the listing', () => {
  const rows = extensionRowsOf([offer(), SPOOLS], [], []);

  it('searches the title, the module path, the summary and the author, ignoring case', () => {
    const keys = (typed: string) =>
      filterExtensions(rows, NO_EXTENSIONS_SELECTED, searchTerm(typed)).map(
        (row) => row.title,
      );

    expect(keys('  ACME ')).toEqual(['Spools']);
    expect(keys('prusa3d')).toEqual(['Parts']);
    expect(keys('filament')).toEqual(['Spools']);
    expect(keys('')).toEqual(['Parts', 'Spools']);
    expect(keys('nothing')).toEqual([]);
  });

  it('keeps the rows of every value chosen in a dimension', () => {
    expect(
      filterExtensions(
        rows,
        { source: ['marketplace'], trust: [], state: [] },
        '',
      ).map((row) => row.title),
    ).toEqual(['Parts']);
    expect(
      filterExtensions(
        rows,
        { source: ['marketplace', 'local'], trust: [], state: ['installed'] },
        '',
      ).map((row) => row.title),
    ).toEqual(['Spools']);
  });

  it('counts each value with the other dimension and the search applied', () => {
    const bySource = extensionFacetCounts(
      rows,
      { source: [], trust: [], state: ['installed'] },
      '',
      'source',
      SOURCES,
    );

    // A value that brings nothing reads zero rather than vanishing.
    expect(Object.fromEntries(bySource)).toEqual({
      local: 1,
      marketplace: 0,
      unlisted: 0,
    });

    const byState = extensionFacetCounts(
      rows,
      { source: ['marketplace'], trust: [], state: ['installed'] },
      '',
      'state',
      STATES,
    );

    // A dimension does not narrow its own counts.
    expect(Object.fromEntries(byState)).toEqual({
      update: 0,
      installed: 0,
      available: 1,
    });
  });
});

describe('narrowing by trust', () => {
  const rows = extensionRowsOf(
    [
      offer({ trust: 'official' }),
      offer({
        title: 'Spools',
        module: 'github.com/acme/spools',
        namespace: 'com.github.aux.acme.spools',
        trust: 'community',
      }),
    ],
    [installed('com.github.aux.me.by-hand', 'by-hand')],
    [],
  );

  it('keeps the rows the marketplace vouches for at a chosen level', () => {
    expect(
      filterExtensions(
        rows,
        { source: [], trust: ['official'], state: [] },
        '',
      ).map((row) => row.title),
    ).toEqual(['Parts']);
  });

  it('counts every level, and a row with no trust under none of them', () => {
    const byTrust = extensionFacetCounts(
      rows,
      { source: [], trust: [], state: [] },
      '',
      'trust',
      TRUSTS,
    );

    expect(byTrust.get('official')).toBe(1);
    expect(byTrust.get('verified')).toBe(0);
    expect(byTrust.get('community')).toBe(1);
  });
});

describe('community extensions nobody chose to list', () => {
  const community = {
    module: 'github.com/jane/nozzle-watch',
    namespace: 'com.github.aux.jane.nozzle-watch',
    title: 'Nozzle watch',
    trust: 'community',
  };
  // An available unverified row, an installed one, an available trusted one,
  // an official one, and one from this server's own index.
  const rows = extensionRowsOf(
    [
      offer(community),
      offer({
        ...community,
        module: 'github.com/jane/bed-watch',
        namespace: 'com.github.aux.jane.bed-watch',
        installed: true,
        installed_version: '0.2.0',
      }),
      offer({ trust: 'verified' }),
      offer({
        module: 'github.com/prusa3d/spools',
        namespace: 'com.github.aux.prusa3d.spools',
        trust: 'official',
      }),
      offer({
        module: 'github.com/me/local',
        namespace: 'com.github.aux.me.local',
        origin: 'local',
      }),
    ],
    [],
    [],
  );

  it('leaves out every community row that is not installed, trusted ones too', () => {
    expect(rows.map((row) => hidesCommunity(row, false))).toEqual([
      true,
      false,
      true,
      false,
      false,
    ]);
  });

  it('leaves out nothing once a person chose to list them', () => {
    expect(rows.some((row) => hidesCommunity(row, true))).toBe(false);
  });
});

describe('the order of the listing', () => {
  it('puts updates first, then installed modules, then the rest, each by title', () => {
    const rows = extensionRowsOf(
      [
        offer({ title: 'Zeta', module: 'z', namespace: 'z.z' }),
        offer({ title: 'alpha', module: 'a', namespace: 'a.a' }),
        offer({
          title: 'Middle',
          module: 'm',
          namespace: 'm.m',
          installed: true,
          installed_version: '0.2.0',
        }),
        offer({
          title: 'Old',
          module: 'o',
          namespace: 'o.o',
          installed: true,
          installed_version: '0.1.0',
        }),
      ],
      [],
      [],
    );

    expect([...rows].sort(compareExtensions).map((row) => row.title)).toEqual([
      'Old',
      'Middle',
      'alpha',
      'Zeta',
    ]);
  });
});
