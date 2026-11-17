import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import StepList from './step-list.svelte';
import type { StepListStep } from './types';

const steps: StepListStep[] = [
  { id: 'file', label: 'Prepare the file', description: 'Download it' },
  { id: 'printer', label: 'On the printer', description: 'A few taps' },
  { id: 'approve', label: 'Approve the printer' },
];

const items = (): HTMLElement[] =>
  Array.from(
    document.querySelectorAll<HTMLElement>('[data-slot="step-list-item"]'),
  );

describe('StepList', () => {
  it('draws each step behind, under or ahead of the person', async () => {
    await render(StepList, { props: { label: 'Steps', steps, current: 1 } });

    expect(items().map((one) => one.dataset.state)).toEqual([
      'done',
      'current',
      'upcoming',
    ]);
    const marked = document.querySelectorAll('[aria-current="step"]');
    expect(marked).toHaveLength(1);
    expect(marked[0]?.textContent).toContain('On the printer');
  });

  it('shows a tick for a done step and a number for the others', async () => {
    await render(StepList, { props: { label: 'Steps', steps, current: 1 } });

    const markers = Array.from(
      document.querySelectorAll('[data-slot="step-list-marker"]'),
    );
    expect(markers[0]?.querySelector('svg')).not.toBe(null);
    expect(markers[1]?.textContent.trim()).toBe('2');
    expect(markers[2]?.textContent.trim()).toBe('3');
  });

  it('draws the line under each marker but the last, only when asked', async () => {
    const { rerender } = await render(StepList, {
      props: { label: 'Steps', steps, current: 1 },
    });
    expect(
      document.querySelectorAll('[data-slot="step-list-line"]'),
    ).toHaveLength(0);

    await rerender({ label: 'Steps', steps, current: 1, connected: true });
    expect(
      document.querySelectorAll('[data-slot="step-list-line"]'),
    ).toHaveLength(2);
  });

  it('draws a description only for a step that has one', async () => {
    await render(StepList, { props: { label: 'Steps', steps, current: 0 } });

    await expect.element(page.getByText('Download it')).toBeVisible();
    expect(
      document.querySelectorAll('[data-slot="step-list-description"]'),
    ).toHaveLength(2);
  });

  it('is a plain named list with no buttons when nothing can be picked', async () => {
    await render(StepList, { props: { label: 'Steps', steps, current: 2 } });

    await expect
      .element(page.getByRole('list', { name: 'Steps' }))
      .toBeInTheDocument();
    expect(page.getByRole('navigation').query()).toBe(null);
    expect(page.getByRole('button').query()).toBe(null);
  });

  it('opens a passed step and refuses the steps ahead when it can pick', async () => {
    const picked: string[] = [];
    await render(StepList, {
      props: {
        label: 'Steps',
        steps,
        current: 1,
        onpick: (id: string) => {
          picked.push(id);
        },
      },
    });

    await expect
      .element(page.getByRole('navigation', { name: 'Steps' }))
      .toBeInTheDocument();
    await page.getByRole('button', { name: /Prepare the file/ }).click();
    await expect
      .element(page.getByRole('button', { name: /Approve the printer/ }))
      .toBeDisabled();

    expect(picked).toEqual(['file']);
  });

  it('opens a step ahead that the person has already reached', async () => {
    const picked: string[] = [];
    await render(StepList, {
      props: {
        label: 'Steps',
        steps,
        current: 0,
        reached: 1,
        onpick: (id: string) => {
          picked.push(id);
        },
      },
    });

    expect(items().map((one) => one.dataset.state)).toEqual([
      'current',
      'upcoming',
      'upcoming',
    ]);
    await page.getByRole('button', { name: /On the printer/ }).click();
    await expect
      .element(page.getByRole('button', { name: /Approve the printer/ }))
      .toBeDisabled();
    await expect
      .element(page.getByRole('button', { name: /Prepare the file/ }))
      .toBeDisabled();

    expect(picked).toEqual(['printer']);
  });

  it('has nothing axe refuses in either form', async () => {
    const { container, rerender } = await render(StepList, {
      props: { label: 'Steps', steps, current: 1, connected: true },
    });
    await expectNoViolations(container);

    await rerender({
      label: 'Steps',
      steps,
      current: 1,
      onpick: () => undefined,
    });
    await expectNoViolations(container);
  });
});
