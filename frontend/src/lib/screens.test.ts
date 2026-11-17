// Which destinations this build answers with a screen.
import { expect, test } from 'vitest';
import { FEATURES } from '@prusa/connect-ui/domain';

import { PATHS, routes, settingsRoute } from './routes';
import { SCREENS } from './screens';

const answers = (path: string): boolean =>
  SCREENS.some((route) => route.pattern === path);

// The root draws nothing of its own.
test('the root is answered by a screen of its own', () => {
  const root = SCREENS.find((route) => route.pattern === PATHS.home);
  const printers = SCREENS.find((route) => route.pattern === PATHS.printers);

  expect(root).toBeDefined();
  expect(root?.component).not.toBe(printers?.component);
});

test('no pattern is listed twice', () => {
  const patterns = SCREENS.map((route) => route.pattern);

  expect(new Set(patterns).size).toBe(patterns.length);
});

// The sweep this file exists for.
test('every rail destination has a screen', () => {
  const offered = [
    ...routes().map((item) => item.href),
    settingsRoute().href,
    ...(settingsRoute().children ?? []).map((item) => item.href),
  ];

  expect(offered.length).toBeGreaterThan(5);
  for (const href of offered)
    expect([href, answers(href)]).toEqual([href, true]);
});

test('the printers page and the page that adds one are both answered', () => {
  expect(answers(PATHS.printers)).toBe(true);
  expect(answers(PATHS.printersAdd)).toBe(true);
});

test('the storage page is answered', () => {
  expect(answers(PATHS.storage)).toBe(true);
});

test('the jobs page and one print are both answered', () => {
  expect(answers(PATHS.jobs)).toBe(true);
  expect(answers(PATHS.jobDetail)).toBe(true);
});

// ADR-20260930-rq4.
test('the tags page is answered only while tags are shown', () => {
  expect(answers(PATHS.tags)).toBe(FEATURES.tags);
});

test('every room the Settings entry offers is answered', () => {
  expect(answers(PATHS.settings)).toBe(true);
  expect(answers(PATHS.settingsAccount)).toBe(true);
  expect(answers(PATHS.settingsAppearance)).toBe(true);
  expect(answers(PATHS.settingsExtensions)).toBe(true);
  expect(answers(PATHS.settingsTokens)).toBe(true);
  expect(answers(PATHS.settingsPeople)).toBe(true);
  expect(answers(PATHS.settingsOldPeople)).toBe(true);
  expect(answers(PATHS.settingsOldNetwork)).toBe(true);
  expect(answers(PATHS.settingsServer)).toBe(true);
  expect(answers(PATHS.settingsServerOverview)).toBe(true);
  expect(answers(PATHS.settingsServerHealth)).toBe(true);
  expect(answers(PATHS.settingsServerUpdates)).toBe(true);
  expect(answers(PATHS.settingsServerBackups)).toBe(true);
  expect(answers(PATHS.settingsServerNetwork)).toBe(true);
  expect(answers(PATHS.settingsMaintenance)).toBe(true);
});

test('the page of one module is answered, at any of its tabs', () => {
  expect(answers(PATHS.extensionDetail)).toBe(true);
  expect(answers(PATHS.extensionDetailTab)).toBe(true);
});
