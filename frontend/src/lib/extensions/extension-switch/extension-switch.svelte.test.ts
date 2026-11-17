// The switch of one row: an approved module has `enabled` written, an
// unapproved one opens the install question instead.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { ResourceInstance } from '@prusa/connect-ui/data';

import type { ModuleCatalogue } from '../../stores/module-catalogue.svelte';
import type { ExtensionRow } from '../rows';
import ExtensionSwitch from './extension-switch.svelte';

const rowOf = (approved: boolean, memory = false): ExtensionRow =>
  ({
    key: 'k',
    module: 'github.com/acme/spools',
    title: 'Spools',
    summary: '',
    author: '',
    source: 'local',
    state: 'installed',
    version: '1.0.0',
    installedVersion: '0.9.0',
    platforms: [],
    permissions: [],
    pages: [],
    offer: { module: 'github.com/acme/spools' },
    instance: {
      kind: 'asset',
      name: 'spools',
      namespace: 'com.github.aux.acme.spools',
      enabled: approved,
      source: 'overlay',
      module: approved ? { asked: {} } : {},
      status: {
        state: approved ? 'ready' : 'disabled',
        ...(memory ? { memory: { budget_bytes: 1 } } : {}),
      },
    } as unknown as ResourceInstance,
  }) as unknown as ExtensionRow;

const catalogueOf = (writeInstance: () => unknown): ModuleCatalogue =>
  ({
    mayConfigure: () => true,
    writeInstance,
    writing: undefined,
    acting: undefined,
  }) as unknown as ModuleCatalogue;

describe('the switch of an extension', () => {
  it('asks the memory question before switching a service module on', async () => {
    const ask = vi.fn();
    const writeInstance = vi.fn();
    const on = rowOf(true, true);
    const row = {
      ...on,
      instance: { ...on.instance, enabled: false } as ResourceInstance,
    };
    await render(ExtensionSwitch, {
      props: { row, catalogue: catalogueOf(writeInstance), ask },
    });

    await page.getByRole('switch').click();

    expect(ask).toHaveBeenCalledWith(row, 'enable');
    expect(writeInstance).not.toHaveBeenCalled();
  });

  it('asks the install question for an unapproved module instead of writing', async () => {
    const ask = vi.fn();
    const writeInstance = vi.fn();
    const row = rowOf(false);
    await render(ExtensionSwitch, {
      props: { row, catalogue: catalogueOf(writeInstance), ask },
    });

    await page.getByRole('switch').click();

    expect(ask).toHaveBeenCalledWith(row, 'install', '0.9.0');
    expect(writeInstance).not.toHaveBeenCalled();
  });

  it('writes enabled for an approved module', async () => {
    const writeInstance = vi.fn(() => Promise.resolve(true));
    const row = rowOf(true);
    await render(ExtensionSwitch, {
      props: { row, catalogue: catalogueOf(writeInstance), ask: vi.fn() },
    });

    await page.getByRole('switch').click();

    expect(writeInstance).toHaveBeenCalledWith(row.instance, {
      enabled: false,
    });
  });
});
