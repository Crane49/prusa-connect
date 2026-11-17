import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import Stepper from './stepper.svelte';
import type { StepperStep } from './types';

const steps: StepperStep[] = [
  {
    id: 'account',
    label: 'Create your account',
    description: 'Name and password',
  },
  { id: 'server', label: 'Name this server', description: 'What people see' },
  { id: 'device', label: 'Add a device', description: 'Your first printer' },
];

const items = (): HTMLElement[] =>
  Array.from(
    document.querySelectorAll<HTMLElement>('[data-slot="stepper-item"]'),
  );

const nextFrame = () =>
  new Promise<void>((resolve) => {
    requestAnimationFrame(() => {
      resolve();
    });
  });

describe('Stepper', () => {
  it('draws one item per step, behind, under and ahead of the person', async () => {
    await render(Stepper, { props: { steps, current: 1 } });

    expect(items().map((one) => one.dataset.state)).toEqual([
      'done',
      'current',
      'upcoming',
    ]);
  });

  it('marks the step the person is on for a screen reader', async () => {
    await render(Stepper, { props: { steps, current: 1 } });

    const marked = document.querySelectorAll('[aria-current="step"]');
    expect(marked).toHaveLength(1);
    expect(marked[0]?.textContent).toContain('Name this server');
  });

  it('reads each step its state, because the tick and the fill are only seen', async () => {
    await render(Stepper, { props: { steps, current: 1 } });

    const [done, current, upcoming] = items();
    expect(done?.textContent).toContain('done');
    expect(current?.textContent).toContain('current step');
    expect(upcoming?.textContent).toContain('to do');
  });

  it('names its list, so a screen reader can find it', async () => {
    await render(Stepper, {
      props: { steps, current: 0, label: 'The steps of the first run' },
    });

    await expect
      .element(page.getByRole('list', { name: 'The steps of the first run' }))
      .toBeInTheDocument();
  });

  it('clamps a step index outside the list rather than losing the person', async () => {
    const past = await render(Stepper, { props: { steps, current: 9 } });
    expect(items().map((one) => one.dataset.state)).toEqual([
      'done',
      'done',
      'current',
    ]);
    await past.unmount();

    await render(Stepper, { props: { steps, current: -3 } });
    expect(items().map((one) => one.dataset.state)).toEqual([
      'current',
      'upcoming',
      'upcoming',
    ]);
  });

  it('writes the count and hides the labels in the compact form', async () => {
    await render(Stepper, { props: { steps, current: 1, compact: true } });

    await expect.element(page.getByText('Step 2 of 3')).toBeVisible();
    const label = document.querySelector('[data-slot="stepper-label"]');
    if (label === null) throw new Error('no label rendered');
    // Read out and never drawn: the numbers in the row already say it.
    expect(label.parentElement?.className).toContain('sr-only');
  });

  it('drops the descriptions when its own row is too narrow for them', async () => {
    const host = document.createElement('div');
    host.style.inlineSize = '20rem';
    document.body.appendChild(host);

    try {
      await render(Stepper, { target: host, props: { steps, current: 0 } });
      const line = document.querySelector('[data-slot="stepper-description"]');
      if (line === null) throw new Error('no description rendered');
      expect(getComputedStyle(line).display).toBe('none');

      host.style.inlineSize = '40rem';
      await nextFrame();
      expect(getComputedStyle(line).display).toBe('block');
    } finally {
      host.remove();
    }
  });
});
