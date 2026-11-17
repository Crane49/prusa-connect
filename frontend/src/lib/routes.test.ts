import { expect, test } from 'vitest';

import type { AnnouncedPage } from '@prusa/connect-ui/data';
import { matchesPattern, paramsOf } from '@prusa/connect-ui/router';
import { DEFAULT_MODULE_ICON, MODULE_ICONS } from '@prusa/connect-ui/slots';

import {
  EXTENSIONS,
  moduleNav,
  PATHS,
  PATTERNS,
  extensionDetailHref,
  jobsOfPrinterHref,
  settingsRoute,
  storageFileHref,
  storageHref,
} from './routes';

const announced = (over: Partial<AnnouncedPage> = {}): AnnouncedPage => ({
  namespace: 'com.github.aux.acme.spools',
  path: 'spools',
  title: 'Spools',
  hidden: false,
  href: '/m/com.github.aux.acme.spools/spools',
  ...over,
});

test('a declared page becomes one rail entry under Extensions', () => {
  const items = moduleNav([announced()]);

  expect(items).toHaveLength(1);
  expect(items[0]?.id).toBe('module:com.github.aux.acme.spools/spools');
  expect(items[0]?.label).toBe('Spools');
  expect(items[0]?.href).toBe('/m/com.github.aux.acme.spools/spools');
  expect(items[0]?.group).toBe(EXTENSIONS);
  // The collapsed rail is icons alone, so an entry without one is a blank row.
  expect(items[0]?.icon).toBeDefined();
});

test('a page draws the icon its manifest names, and the puzzle for any other', () => {
  const [named, unknown, none, inherited] = moduleNav([
    announced({ namespace: 'a', icon: 'swatch-book' }),
    announced({ namespace: 'b', icon: 'no-such-icon' }),
    announced({ namespace: 'c' }),
    announced({ namespace: 'd', icon: 'constructor' }),
  ]);

  expect(named?.icon).toBe(MODULE_ICONS['swatch-book']);
  expect(unknown?.icon).toBe(DEFAULT_MODULE_ICON);
  expect(none?.icon).toBe(DEFAULT_MODULE_ICON);
  expect(inherited?.icon).toBe(DEFAULT_MODULE_ICON);
});

test('a hidden page keeps its route and stays off the rail', () => {
  expect(moduleNav([announced({ hidden: true })])).toEqual([]);
});

test('the order is the order the listing gave', () => {
  const items = moduleNav([
    announced({ namespace: 'acme.shelf', title: 'Shelf' }),
    announced({ title: 'Spools' }),
  ]);

  expect(items.map((item) => item.label)).toEqual(['Shelf', 'Spools']);
});

test('a module with several pages gets one row under its title, opening the first', () => {
  const [row, other] = moduleNav([
    announced({
      path: 'plan',
      title: 'Plan',
      module: 'Farm',
      href: '/m/farm/plan',
      namespace: 'farm',
    }),
    announced({ namespace: 'other' }),
    announced({
      path: 'jobs',
      title: 'Jobs',
      module: 'Farm',
      href: '/m/farm/jobs',
      namespace: 'farm',
    }),
  ]);

  expect(row?.label).toBe('Farm');
  expect(row?.href).toBe('/m/farm/plan');
  expect(row?.group).toBe(EXTENSIONS);
  expect(row?.icon).toBeDefined();
  expect(row?.children?.map((child) => child.label)).toEqual(['Plan', 'Jobs']);
  expect(other?.label).toBe('Spools');
});

test('a module with one page left in view keeps a plain row', () => {
  const items = moduleNav([
    announced({ module: 'Acme' }),
    announced({ path: 'shelf', title: 'Shelf', module: 'Acme', hidden: true }),
  ]);

  expect(items.map((item) => [item.label, item.children])).toEqual([
    ['Spools', undefined],
  ]);
});

test('the matcher answers a module page and captures both parts', () => {
  expect(PATTERNS).toContain(PATHS.modulePage);
  expect(
    matchesPattern(PATHS.modulePage, '/m/com.github.aux.acme.spools/spools'),
  ).toBe(true);
  // One label with no slash, so a deeper address is not this pattern's.
  expect(matchesPattern(PATHS.modulePage, '/m/acme.spools/a/b')).toBe(false);
});

test('no pattern above the module page swallows it', () => {
  const found = PATTERNS.find((pattern) =>
    matchesPattern(pattern, '/m/acme.spools/spools'),
  );

  expect(found).toBe(PATHS.modulePage);
});

/** The one pattern the router takes an address to, as it takes a click. */
const patternOf = (path: string): string | undefined =>
  PATTERNS.find((pattern) => matchesPattern(pattern, path));

// The address of a file names its storage, so a server with two storages
// shows the file the reader asked for. The path is one segment, so a file
// deep in folders is still an address the router takes.
test('a file address names its storage and its path', () => {
  const href = storageFileHref('usb', 'models/parts/bracket.bgcode');

  expect(patternOf(href)).toBe(PATHS.storageFile);
  expect(paramsOf(PATHS.storageFile, href)).toEqual({
    storage: 'usb',
    path: 'models/parts/bracket.bgcode',
  });
});

test('a folder address names its storage and its folder', () => {
  expect(patternOf(storageHref('usb'))).toBe(PATHS.storageRoot);
  expect(paramsOf(PATHS.storageRoot, storageHref('usb'))).toEqual({
    storage: 'usb',
  });

  const folder = storageHref('usb', 'models/parts');
  expect(patternOf(folder)).toBe(PATHS.storageFolder);
  expect(paramsOf(PATHS.storageFolder, folder)).toEqual({
    storage: 'usb',
    path: 'models/parts',
  });
});

// The storage page itself, and the first storage's own address, are the
// same page with nothing chosen.
test('the storage page without a storage is its own address', () => {
  expect(storageHref()).toBe(PATHS.storage);
  expect(patternOf(PATHS.storage)).toBe(PATHS.storage);
});

test('the prints of one printer are an address of their own', () => {
  const href = jobsOfPrinterHref('shop/mk4:2');

  expect(patternOf(href)).toBe(PATHS.printerJobs);
  expect(paramsOf(PATHS.printerJobs, href)).toEqual({ id: 'shop/mk4:2' });
});

// An address bookmarked before the file page named its storage has a screen
// of its own, so it never opens a storage called file by mistake.
test('the old file address has a pattern of its own', () => {
  expect(patternOf('/storage/file')).toBe(PATHS.storageOldFile);
});

test('the page of one module is at its namespace, and at a tab when one is named', () => {
  expect(extensionDetailHref('com.github.aux.acme.spools')).toBe(
    '/settings/extensions/com.github.aux.acme.spools',
  );
  expect(extensionDetailHref('com.github.aux.acme.spools', 'settings')).toBe(
    '/settings/extensions/com.github.aux.acme.spools/settings',
  );
});

test('server settings child routes are matched without reviving maintenance in the rail', () => {
  expect(patternOf(PATHS.settingsServerHealth)).toBe(
    PATHS.settingsServerHealth,
  );
  expect(patternOf(PATHS.settingsServerUpdates)).toBe(
    PATHS.settingsServerUpdates,
  );
  expect(patternOf(PATHS.settingsServerBackups)).toBe(
    PATHS.settingsServerBackups,
  );

  const serverRooms = (settingsRoute().children ?? []).filter(
    (item) => item.id === 'settings-server',
  );
  expect(serverRooms).toHaveLength(1);
  expect(
    (settingsRoute().children ?? []).some(
      (item) => item.href === PATHS.settingsMaintenance,
    ),
  ).toBe(false);
});
