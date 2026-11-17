// The operator's memory budget: what it shows, which numbers it refuses, and
// what Save and the reset write.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ExtensionMemory from './extension-memory.svelte';

const estimate = {
  mib: 256,
  source: 'author',
  overPool: false,
  committedMiB: 256,
} as const;

const props = (extra: Record<string, unknown> = {}) => ({
  estimate,
  overBudget: false,
  onOverMemory: 'stop' as const,
  saving: false,
  onchange: vi.fn(),
  onbudget: vi.fn(),
  ...extra,
});

describe('ExtensionMemory usage', () => {
  it('reads the use against the budget, coloured by how close it is', async () => {
    await render(ExtensionMemory, {
      props: props({ usedBytes: 300 * 1_048_576, overBudget: true }),
    });

    const meter = page.getByRole('progressbar', { name: 'Memory used' });
    await expect
      .element(meter)
      .toHaveAttribute('aria-valuetext', '300 MiB of 256 MiB');
    await expect.element(meter).toHaveAttribute('aria-valuenow', '256');
    await expect.element(meter).toHaveAttribute('data-tone', 'destructive');
  });

  it('turns amber close to the budget', async () => {
    await render(ExtensionMemory, {
      props: props({ usedBytes: 220 * 1_048_576, nearBudget: true }),
    });

    await expect
      .element(page.getByRole('progressbar', { name: 'Memory used' }))
      .toHaveAttribute('data-tone', 'warning');
  });

  it('shows only the budget when the use is unknown', async () => {
    await render(ExtensionMemory, { props: props() });

    await expect.element(page.getByText('256 MiB budget')).toBeVisible();
    await expect.element(page.getByRole('progressbar')).not.toBeInTheDocument();
  });
});

const field = () => page.getByRole('spinbutton', { name: 'Memory budget' });
const save = () => page.getByRole('button', { name: 'Save', exact: true });
const reset = () =>
  page.getByRole('button', { name: 'Use the recommendation' });

describe('ExtensionMemory budget', () => {
  it('is empty with the recommendation named, and saves a valid number', async () => {
    const p = props({ recommendedMiB: 256, running: true });
    await render(ExtensionMemory, { props: p });

    await expect.element(field()).toHaveValue(null);
    await expect
      .element(page.getByText(/author's recommendation, 256 MiB/))
      .toBeVisible();
    await expect
      .element(page.getByText(/Saving restarts the module/))
      .toBeVisible();
    await expect.element(save()).toBeDisabled();
    await expect.element(reset()).not.toBeInTheDocument();

    await field().fill('512');
    await save().click();
    expect(p.onbudget).toHaveBeenCalledWith(512);
  });

  it('refuses a number out of range', async () => {
    await render(ExtensionMemory, { props: props() });

    await field().fill('15');
    await expect.element(page.getByText(/from 16 to 8192/)).toBeVisible();
    await expect.element(save()).toBeDisabled();
    const describedBy = field().element().getAttribute('aria-describedby');
    const described = (describedBy ?? '')
      .split(' ')
      .map((name) => document.getElementById(name)?.textContent.trim());
    expect(described).toContain('Enter a whole number of MiB from 16 to 8192.');
    await field().fill('8193');
    await expect.element(save()).toBeDisabled();
    await field().fill('8192');
    await expect.element(save()).toBeEnabled();
  });

  it('shows the operator value, and the reset writes 0', async () => {
    const p = props({ budgetMiB: 512 });
    await render(ExtensionMemory, { props: p });

    await expect.element(field()).toHaveValue(512);
    await expect.element(save()).toBeDisabled();
    await expect
      .element(page.getByText(/server default, 128 MiB/))
      .toBeVisible();
    await reset().click();
    expect(p.onbudget).toHaveBeenCalledWith(0);
  });
});

const failuresField = () => page.getByRole('spinbutton', { name: 'Failures' });
const saveSwitchOff = () =>
  page.getByRole('button', { name: 'Save switch-off' });
const useServer = () =>
  page.getByRole('button', { name: 'Use the server default' });

describe('ExtensionMemory switch-off', () => {
  it('shows the server value, and saves a count of 0 with the window', async () => {
    const onswitchoff = vi.fn();
    await render(ExtensionMemory, {
      props: props({
        switchOff: { failures: 3, windowSeconds: 3600, source: 'default' },
        onswitchoff,
      }),
    });

    await expect.element(failuresField()).toHaveValue(3);
    await expect
      .element(page.getByRole('group', { name: 'Automatic switch-off' }))
      .toBeVisible();
    await expect
      .element(page.getByText(/0 never switches it off/))
      .toBeVisible();
    await expect.element(saveSwitchOff()).toBeDisabled();
    await expect.element(useServer()).not.toBeInTheDocument();

    await failuresField().fill('101');
    await expect.element(page.getByText(/from 0 to 100/)).toBeVisible();
    await expect.element(saveSwitchOff()).toBeDisabled();

    await failuresField().fill('0');
    await saveSwitchOff().click();
    expect(onswitchoff).toHaveBeenCalledWith({
      failures: 0,
      window_seconds: 3600,
    });
  });

  it('returns an operator value to the server default', async () => {
    const onswitchoff = vi.fn();
    await render(ExtensionMemory, {
      props: props({
        switchOff: { failures: 0, windowSeconds: 21600, source: 'operator' },
        onswitchoff,
      }),
    });

    await useServer().click();
    expect(onswitchoff).toHaveBeenCalledWith(undefined);
  });

  it('keeps what was typed when the write fails', async () => {
    const onswitchoff = vi.fn(() => Promise.resolve(false));
    const onbudget = vi.fn(() => Promise.resolve(false));
    await render(ExtensionMemory, {
      props: props({
        switchOff: { failures: 3, windowSeconds: 3600, source: 'default' },
        onswitchoff,
        onbudget,
      }),
    });

    await failuresField().fill('7');
    await saveSwitchOff().click();
    expect(onswitchoff).toHaveBeenCalledOnce();
    await expect.element(failuresField()).toHaveValue(7);

    await field().fill('512');
    await save().click();
    expect(onbudget).toHaveBeenCalledWith(512);
    await expect.element(field()).toHaveValue(512);
  });
});
