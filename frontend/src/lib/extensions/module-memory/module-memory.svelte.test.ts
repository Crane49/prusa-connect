// A service module's memory cell: unknown use is not zero, and the bar and
// the notes follow what the server reported.
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { ResourceMemory } from '../stops';
import ModuleMemory from './module-memory.svelte';

const MIB = 1_048_576;

const memory = (over: Partial<ResourceMemory> = {}): ResourceMemory => ({
  budget_bytes: 128 * MIB,
  budget_source: 'default',
  failures_in_window: 0,
  held_until_restart: false,
  near_budget: false,
  on_over_memory: 'stop',
  over_budget: false,
  switch_off_failures: 3,
  switch_off_source: 'default',
  switch_off_window_seconds: 3600,
  ...over,
});

describe('ModuleMemory', () => {
  it('unknown memory is not zero', async () => {
    await render(ModuleMemory, { props: { running: true, memory: memory() } });

    await expect.element(page.getByText('Unknown / 128 MiB')).toBeVisible();
    const bar = document.querySelector('[data-part="memory-bar"]');
    expect(bar?.getAttribute('data-state')).toBe('indeterminate');
    // An empty track: no fill, so it cannot read as full.
    expect(bar?.children).toHaveLength(0);
    expect(document.querySelector('[role="progressbar"]')).toBeNull();
  });

  it('names only the budget of a module that is not running', async () => {
    await render(ModuleMemory, {
      props: { running: false, memory: memory() },
    });

    await expect.element(page.getByText('128 MiB budget')).toBeVisible();
    expect(document.body.textContent).not.toContain('Unknown');
    expect(document.querySelector('[data-part="memory-bar"]')).toBeNull();
  });

  it('reads used against the budget and turns amber near it', async () => {
    await render(ModuleMemory, {
      props: {
        running: true,
        memory: memory({ used_bytes: 110 * MIB, near_budget: true }),
      },
    });

    await expect.element(page.getByText('110 MiB / 128 MiB')).toBeVisible();
    await expect
      .element(page.getByRole('progressbar'))
      .toHaveAttribute('data-tone', 'warning');
  });

  it('names the failures and the window in force', async () => {
    await render(ModuleMemory, {
      props: {
        running: true,
        memory: memory({
          used_bytes: MIB,
          failures_in_window: 2,
          switch_off_window_seconds: 600,
        }),
      },
    });

    await expect
      .element(page.getByText(/^2 failures within 10 min/u))
      .toBeVisible();
  });

  it('warns for a module over its budget that only warns', async () => {
    await render(ModuleMemory, {
      props: {
        running: true,
        memory: memory({
          used_bytes: 200 * MIB,
          over_budget: true,
          on_over_memory: 'warn',
        }),
      },
    });

    await expect
      .element(page.getByText(/keeps running because it is set to only warn/u))
      .toBeVisible();
  });
});
