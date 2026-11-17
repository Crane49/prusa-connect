// The extensions an install also adds, and the refusals of a plan.
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ExtensionPlan from './extension-plan.svelte';
import {
  adds,
  conflict,
  fileTypeConflict,
  loading,
  notOffered,
} from './fixtures';

describe('ExtensionPlan', () => {
  it('lists each extension it adds, who requires it and what it asks for', async () => {
    await render(ExtensionPlan, { props: { plan: adds } });

    await expect
      .element(
        page.getByText(
          /also needs 1 more extension, which is installed with it/u,
        ),
      )
      .toBeVisible();
    await expect.element(page.getByText('Reader')).toBeVisible();
    await expect
      .element(page.getByText('Required by slicer ^1.2'))
      .toBeVisible();
    await expect
      .element(page.getByText('Permission read_storage'))
      .toBeVisible();
    await expect
      .element(page.getByText(/Your approval covers what every extension/u))
      .toBeVisible();
  });

  it('says it is reading the plan', async () => {
    await render(ExtensionPlan, { props: { plan: loading } });

    await expect.element(page.getByRole('status')).toBeVisible();
  });

  it('says who needs what when no release fits', async () => {
    await render(ExtensionPlan, { props: { plan: conflict } });

    await expect
      .element(
        page.getByText(/^No release fits every extension that needs it: /u),
      )
      .toBeVisible();
  });

  it('says an extension the install needs is in no list', async () => {
    await render(ExtensionPlan, { props: { plan: notOffered } });

    await expect
      .element(page.getByText(/is in no list this server reads/u))
      .toBeVisible();
  });

  // ADR-20261027-wvj rule 27.
  it('names the enabled extension that handles the same files', async () => {
    await render(ExtensionPlan, { props: { plan: fileTypeConflict } });

    await expect
      .element(page.getByText('reader and projects both read .3mf files.'))
      .toBeVisible();
    await expect
      .element(
        page.getByText('reader and projects both draw the page of .3mf files.'),
      )
      .toBeVisible();
    expect(document.querySelector('[data-part="plan"]')).toBeNull();
  });

  it('draws nothing for a plan that adds nothing', async () => {
    await render(ExtensionPlan, {
      props: { plan: { state: 'ready', dependencies: [], conflicts: [] } },
    });

    expect(document.querySelector('[data-part="plan"]')).toBeNull();
  });
});
