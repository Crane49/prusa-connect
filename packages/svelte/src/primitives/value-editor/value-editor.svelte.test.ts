import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import { NOZZLE_PRESETS } from '../../test/nozzle-presets';
import ValueEditor from './value-editor.svelte';

const editor = (props: Record<string, unknown> = {}) =>
  render(ValueEditor, {
    props: {
      label: 'Nozzle',
      subject: 'Tool 1',
      currentLabel: '234.6°C',
      target: 235,
      unit: '°C',
      min: 0,
      max: 300,
      step: 5,
      presets: NOZZLE_PRESETS,
      onCommit: vi.fn(),
      ...props,
    },
  });

const trigger = () =>
  page.getByRole('button', { name: 'Set Tool 1 nozzle target' });
// bits-ui puts no role on the surface.
const surface = (): HTMLElement | null =>
  document.querySelector<HTMLElement>('[data-slot="popover-content"]');
const field = () => page.getByRole('spinbutton', { name: 'Target (°C)' });
const preset = (name: string) => page.getByRole('button', { name });

const openEditor = async (): Promise<void> => {
  await trigger().click();
  await expect.poll(() => surface()).not.toBe(null);
};

describe('ValueEditor', () => {
  it('names the value it sets, and opens a surface headed by it', async () => {
    await editor();

    await openEditor();

    await expect.element(page.getByText('Tool 1 · Nozzle')).toBeVisible();
    await expect.element(page.getByText('Now 234.6°C')).toBeVisible();
    await expect.element(field()).toHaveValue(235);
    await expectNoViolations(surface()!);
  });

  it('adds the live target to the trigger when the caller asks for it', async () => {
    await editor({ announceTarget: true });

    await expect
      .element(
        page.getByRole('button', {
          name: 'Set Tool 1 nozzle target, currently 235°C',
        }),
      )
      .toBeInTheDocument();
  });

  it('cannot be opened while the value cannot be set', async () => {
    await editor({ disabled: true });

    await expect.element(trigger()).toBeDisabled();
  });

  // A refusal the caller has nowhere else to put.
  it('answers with the refusal in place of the controls', async () => {
    await editor({ reason: 'This printer is locked.' });
    await openEditor();

    await expect
      .element(page.getByText('This printer is locked.'))
      .toBeVisible();
    await expect.element(field()).not.toBeInTheDocument();
    await expect.element(preset('PLA')).not.toBeInTheDocument();
    await expect
      .element(page.getByRole('button', { name: 'Set', exact: true }))
      .not.toBeInTheDocument();
    await expectNoViolations(surface()!);
  });

  it('sends nothing until Set is pressed', async () => {
    const onCommit = vi.fn();
    await editor({ onCommit });
    await openEditor();

    await preset('PETG').click();
    await expect.element(field()).toHaveValue(230);
    expect(onCommit).not.toHaveBeenCalled();

    await page.getByRole('button', { name: 'Set', exact: true }).click();
    expect(onCommit).toHaveBeenCalledWith(230, undefined);
  });

  it('throws the draft away when the surface is closed', async () => {
    const onCommit = vi.fn();
    await editor({ onCommit });
    await openEditor();

    await preset('Off').click();
    await expect.element(field()).toHaveValue(0);

    await userEvent.keyboard('{Escape}');
    await expect.poll(surface).toBe(null);
    expect(onCommit).not.toHaveBeenCalled();

    await openEditor();
    await expect.element(field()).toHaveValue(235);
  });

  it('clamps a figure outside the range', async () => {
    const onCommit = vi.fn();
    await editor({ onCommit });
    await openEditor();

    await field().fill('400');
    await page.getByRole('button', { name: 'Set', exact: true }).click();

    expect(onCommit).toHaveBeenCalledWith(300, undefined);
  });

  // Two presets can hold the same figure.
  it('lights only the preset that was pressed when two share a figure', async () => {
    await editor();
    await openEditor();

    expect(NOZZLE_PRESETS.find((entry) => entry.label === 'PLA')?.value).toBe(
      NOZZLE_PRESETS.find((entry) => entry.label === 'PVB')?.value,
    );

    await preset('PLA').click();

    await expect
      .element(preset('PLA'))
      .toHaveAttribute('data-variant', 'default');
    await expect
      .element(preset('PVB'))
      .toHaveAttribute('data-variant', 'secondary');
    await expect.element(field()).toHaveValue(215);
  });

  it('clears the preset highlight when a figure is typed by hand', async () => {
    await editor();
    await openEditor();

    await preset('PLA').click();
    await expect
      .element(preset('PLA'))
      .toHaveAttribute('data-variant', 'default');

    await field().fill('216');

    await expect
      .element(preset('PLA'))
      .toHaveAttribute('data-variant', 'secondary');
  });

  it('keeps a figure typed off the slider step', async () => {
    await editor();
    await openEditor();

    await field().fill('2');
    await new Promise((resolve) => setTimeout(resolve, 50));

    await expect.element(field()).toHaveValue(2);
  });

  it('opens on a target off the slider step and sets it unchanged', async () => {
    const onCommit = vi.fn();
    await editor({ onCommit, target: 47 });
    await openEditor();
    await new Promise((resolve) => setTimeout(resolve, 50));

    await expect.element(field()).toHaveValue(47);
    await page.getByRole('button', { name: 'Set', exact: true }).click();
    expect(onCommit).toHaveBeenCalledWith(47, undefined);
  });

  it('holds every preset one tap away without growing into a page', async () => {
    await editor();
    await openEditor();

    for (const { label } of NOZZLE_PRESETS) {
      await expect.element(preset(label)).toBeInTheDocument();
    }
    expect(surface()!.clientHeight).toBeLessThan(500);
  });

  describe('with choices', () => {
    const choices = [
      { key: '1', subject: 'Tool 1', currentLabel: '185°C', target: 0 },
      {
        key: '2',
        subject: 'Tool 2',
        currentLabel: '240°C',
        target: 240,
        targetLabel: 'Target for T2 (°C)',
      },
    ];

    it('opens on the choices alone', async () => {
      await editor({ subject: undefined, choices });
      await page.getByRole('button', { name: 'Set nozzle target' }).click();

      await expect
        .element(page.getByRole('button', { name: 'Tool 2' }))
        .toBeVisible();
      await expect.element(page.getByRole('slider')).not.toBeInTheDocument();
      await expect.element(field()).not.toBeInTheDocument();
      await expectNoViolations(surface()!);
    });

    it('sets the picked one, and goes back to the choices', async () => {
      const onCommit = vi.fn();
      await editor({ subject: undefined, choices, onCommit });
      await page.getByRole('button', { name: 'Set nozzle target' }).click();

      await page.getByRole('button', { name: 'Tool 2' }).click();
      await expect.element(page.getByText('Tool 2 · Nozzle')).toBeVisible();
      await expect.element(page.getByText('Now 240°C')).toBeVisible();
      const target = page.getByRole('spinbutton', {
        name: 'Target for T2 (°C)',
      });
      await expect.element(target).toHaveValue(240);

      await preset('PLA').click();
      await page.getByRole('button', { name: 'Set', exact: true }).click();
      expect(onCommit).toHaveBeenCalledWith(215, choices[1]);

      await page.getByRole('button', { name: 'Set nozzle target' }).click();
      await page.getByRole('button', { name: 'Tool 1' }).click();
      await page.getByRole('button', { name: 'Back' }).click();
      await expect
        .element(page.getByRole('button', { name: 'Tool 2' }))
        .toBeVisible();
    });
  });
});
