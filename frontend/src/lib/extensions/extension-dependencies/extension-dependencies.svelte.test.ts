// What an extension depends on, how this server meets it, who requires it,
// and how its release stands.
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { conflictStandingOf, standingOf } from '../dependencies';
import ExtensionDependencies from './extension-dependencies.svelte';
import {
  allMet,
  blocked,
  READER,
  requiredBy,
  unmet,
  updatePossible,
  whyReader,
} from './fixtures';

describe('ExtensionDependencies', () => {
  it('names each module by its short name, with the constraint, the release installed and the core', async () => {
    await render(ExtensionDependencies, { props: { dependencies: allMet } });

    await expect
      .element(page.getByText('reader', { exact: true }))
      .toBeVisible();
    await expect.element(page.getByText('^1.2')).toBeVisible();
    await expect.element(page.getByText('Installed 1.4.0')).toBeVisible();
    await expect.element(page.getByText(READER)).toBeVisible();
    await expect.element(page.getByText('Runs on core >=0.9')).toBeVisible();
  });

  it('tells an out-of-range release, a switched-off one and a missing one apart', async () => {
    await render(ExtensionDependencies, { props: { dependencies: unmet } });

    const requires = page.elementLocator(
      document.querySelector('[data-part="dependencies-requires"]')!,
    );
    await expect
      .element(requires.getByText('Installed 1.4.0, out of range'))
      .toBeVisible();
    await expect
      .element(requires.getByText('Installed 1.0.0, switched off'))
      .toBeVisible();
    await expect.element(requires.getByText('Not installed')).toBeVisible();
  });

  it('shows a suggestion with its reason, and a conflict this server meets', async () => {
    await render(ExtensionDependencies, { props: { dependencies: unmet } });

    await expect
      .element(page.getByText('Shows the parts of a plan'))
      .toBeVisible();
    await expect
      .element(page.getByText('Installed 1.3.0, in conflict'))
      .toBeVisible();
    await expect.element(page.getByText('No conflict')).toBeVisible();
  });

  it('lists who requires it and says it stays until they are gone', async () => {
    await render(ExtensionDependencies, {
      props: { dependencies: requiredBy },
    });

    await expect.element(page.getByText('slicer')).toBeVisible();
    await expect.element(page.getByText('planner')).toBeVisible();
    await expect
      .element(
        page.getByText(/cannot be removed, and while any is switched on/u),
      )
      .toBeVisible();
  });

  it('says why it is installed and how its release stands', async () => {
    await render(ExtensionDependencies, {
      props: {
        dependencies: requiredBy,
        namespace: READER,
        why: whyReader,
        outdated: updatePossible,
      },
    });

    await expect
      .element(page.getByText('slicer 1.0.0 needs it (^1.2).'))
      .toBeVisible();
    await expect.element(page.getByText('Newest allowed 1.4.0')).toBeVisible();
    await expect.element(page.getByText('Latest 2.0.0')).toBeVisible();
    await expect.element(page.getByText('An update is possible')).toBeVisible();
  });

  it('says when a constraint blocks the newest release', async () => {
    await render(ExtensionDependencies, {
      props: { dependencies: requiredBy, namespace: READER, outdated: blocked },
    });

    await expect
      .element(page.getByText('Blocked by a constraint'))
      .toBeVisible();
  });

  it('reads the standing of a requirement and of a conflict', () => {
    expect(standingOf({ module: READER, version: '^1' })).toBe('missing');
    expect(
      conflictStandingOf({
        module: READER,
        version: '<2',
        met: false,
        installed: '1.0.0',
      }),
    ).toBe('conflicting');
    expect(
      conflictStandingOf({ module: READER, version: '<2', met: true }),
    ).toBe('clear');
  });
});
