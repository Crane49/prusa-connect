import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import GuidedProbe from '../../test/guided-probe.svelte';
import GuidedDialog from './guided-dialog.svelte';
import type { StepperStep } from '../stepper/types';

const snippet = (html: string) =>
  createRawSnippet(() => ({ render: () => html }));

const steps: StepperStep[] = [
  { id: 'name', label: 'Name it' },
  { id: 'connect', label: 'Connect it' },
  { id: 'check', label: 'Check it' },
];

const body = snippet('<p>Give this printer a name.</p>');
const help = snippet('<a href="/help/add-a-device">What this means</a>');

const panel = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="dialog-content"]');

const footer = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="dialog-footer"]');

describe('GuidedDialog', () => {
  it('names the flow, counts the steps and offers Back and Next', async () => {
    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 1,
        children: body,
      },
    });

    await expect
      .element(page.getByRole('dialog'))
      .toHaveAccessibleName('Add a device');
    await expect.element(page.getByText('Step 2 of 3')).toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Back' }))
      .toBeEnabled();
    await expect
      .element(page.getByRole('button', { name: 'Next' }))
      .toBeEnabled();
  });

  it('refuses Back on the first step and calls back on a later one', async () => {
    let backs = 0;
    const first = await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 0,
        children: body,
        onback: () => {
          backs += 1;
        },
      },
    });
    await expect
      .element(page.getByRole('button', { name: 'Back' }))
      .toBeDisabled();
    await first.unmount();
    await expect.poll(() => panel()).toBeNull();

    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 1,
        children: body,
        onback: () => {
          backs += 1;
        },
      },
    });
    await page.getByRole('button', { name: 'Back' }).click();
    expect(backs).toBe(1);
  });

  it('holds the primary button until the step has what it needs', async () => {
    let nexts = 0;
    const blocked = await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 0,
        canContinue: false,
        children: body,
        onnext: () => {
          nexts += 1;
        },
      },
    });

    const stuck = page.getByRole('button', { name: 'Next' });
    await expect.element(stuck).toBeDisabled();
    expect(nexts).toBe(0);
    await blocked.unmount();
    await expect.poll(() => panel()).toBeNull();

    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 0,
        canContinue: true,
        children: body,
        onnext: () => {
          nexts += 1;
        },
      },
    });
    await page.getByRole('button', { name: 'Next' }).click();
    expect(nexts).toBe(1);
  });

  it('names the primary button Done on the last step', async () => {
    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 2,
        children: body,
      },
    });

    await expect
      .element(page.getByRole('button', { name: 'Done' }))
      .toBeVisible();
  });

  it('swallows a press on the primary button while the answer is in flight', async () => {
    let nexts = 0;
    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 0,
        busy: true,
        children: body,
        onnext: () => {
          nexts += 1;
        },
      },
    });

    const next = page.getByRole('button', { name: 'Next' });
    await expect.element(next).toHaveAttribute('aria-busy', 'true');
    // A raw press, because the driver refuses to click a control that reports
    // `aria-disabled`. Swallowing the press is what is under test.
    const node = next.element();
    if (!(node instanceof HTMLElement)) throw new Error('not an element');
    node.click();
    expect(nexts).toBe(0);
  });

  it('waits with one bar, its message and no buttons to press', async () => {
    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 1,
        status: 'waiting',
        waitingMessage: 'Waiting for the printer to answer.',
        children: body,
        help,
      },
    });

    await expect.element(page.getByRole('progressbar')).toBeVisible();
    await expect
      .element(page.getByText('Waiting for the printer to answer.'))
      .toBeVisible();
    await expect
      .element(page.getByRole('link', { name: 'What this means' }))
      .toBeVisible();
    // The step's own question is gone, and an empty band under the message
    // would read as a control that failed to arrive.
    expect(document.body.textContent).not.toContain('Give this printer a name');
    expect(footer()).toBeNull();
  });

  it('ends on success with a Done button and the help link', async () => {
    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 2,
        status: 'success',
        endTitle: 'The printer is added',
        endMessage: 'It is on the fleet now.',
        children: body,
        help,
      },
    });

    const end = page.getByRole('status');
    await expect.element(end).toBeVisible();
    await expect.element(page.getByText('The printer is added')).toBeVisible();
    await expect
      .element(page.getByRole('link', { name: 'What this means' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Done' }))
      .toBeVisible();
    // The steps are behind the person now, so the count goes away.
    expect(document.body.textContent).not.toContain('Step 3 of 3');
  });

  it('announces a failure and keeps the help link beside it', async () => {
    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 2,
        status: 'failure',
        endTitle: 'The printer did not answer',
        endMessage: 'Check that it is on the same network.',
        children: body,
        help,
      },
    });

    const alert = page.getByRole('alert');
    await expect.element(alert).toBeVisible();
    await expect
      .element(page.getByText('Check that it is on the same network.'))
      .toBeVisible();
    await expect
      .element(page.getByRole('link', { name: 'What this means' }))
      .toBeVisible();
  });

  it('reports one close, whether the person presses Done or Escape', async () => {
    let closes = 0;
    const done = await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 2,
        status: 'success',
        endTitle: 'The printer is added',
        children: body,
        onclose: () => {
          closes += 1;
        },
      },
    });

    await page.getByRole('button', { name: 'Done' }).click();
    await expect.poll(() => panel()).toBeNull();
    expect(closes).toBe(1);
    await done.unmount();

    await render(GuidedDialog, {
      props: {
        open: true,
        title: 'Add a device',
        steps,
        current: 0,
        children: body,
        onclose: () => {
          closes += 1;
        },
      },
    });
    await expect.element(page.getByRole('dialog')).toBeVisible();
    await userEvent.keyboard('{Escape}');
    await expect.poll(() => panel()).toBeNull();
    expect(closes).toBe(2);
  });

  // The two below are the reason a flow feels quick or slow. A step is a
  // question inside a panel that is already open, so the press has to change
  // the question and leave the rest of the panel where it is.
  it('keeps the same panel and touches nothing outside the step body', async () => {
    await render(GuidedProbe, {});
    await expect.element(page.getByText('Step 1 of 3')).toBeVisible();

    const before = panel();
    const body = document.querySelector('[data-slot="dialog-body"]');
    if (before === null || body === null) throw new Error('no panel rendered');

    // Every node added or removed anywhere in the document, minus the ones
    // inside the step's own body. The list has to come back empty.
    const outside: Node[] = [];
    const watcher = new MutationObserver((records) => {
      for (const record of records) {
        if (body.contains(record.target)) continue;
        outside.push(...record.addedNodes, ...record.removedNodes);
      }
    });
    watcher.observe(document.body, { childList: true, subtree: true });

    await page.getByRole('button', { name: 'Next' }).click();
    await expect.element(page.getByText('Step 2 of 3')).toBeVisible();
    await expect.element(page.getByText('Question 2')).toBeVisible();

    await page.getByRole('button', { name: 'Back' }).click();
    await expect.element(page.getByText('Step 1 of 3')).toBeVisible();
    watcher.disconnect();

    expect(outside).toEqual([]);
    // The same element, so the panel was never torn down and rebuilt, and the
    // focus trap and the scroll position inside it are the ones it had.
    expect(panel()).toBe(before);
  });

  it('changes the step on the frame after the press', async () => {
    await render(GuidedProbe, {});
    await expect.element(page.getByText('Step 1 of 3')).toBeVisible();

    let frames = 0;
    const changed = new Promise<number>((resolve) => {
      const look = (): void => {
        const tick = (): void => {
          if (document.body.textContent.includes('Question 2')) {
            resolve(frames);

            return;
          }
          frames += 1;
          if (frames > 60) {
            resolve(frames);

            return;
          }
          requestAnimationFrame(tick);
        };
        requestAnimationFrame(tick);
      };
      window.addEventListener('pointerdown', look, {
        capture: true,
        once: true,
      });
    });

    await page.getByRole('button', { name: 'Next' }).click();
    expect(await changed).toBeLessThanOrEqual(1);
  });
});
