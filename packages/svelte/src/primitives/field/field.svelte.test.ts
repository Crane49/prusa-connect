import { afterEach, describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import RovingTabindexProbe from '../../test/roving-tabindex-probe.svelte';
import Input from '../input/input.svelte';
import { ToggleGroup } from '../toggle-group';
import Field from './field.svelte';
import type { FieldControl } from './types';

const hosts: HTMLElement[] = [];

/** A host of its own per field, so two fields on the page stay tellable apart. */
const inside = (): HTMLElement => {
  const host = document.createElement('div');
  document.body.append(host);
  hosts.push(host);
  return host;
};

const controlOf = (host: HTMLElement): HTMLInputElement => {
  const found = host.querySelector('input');
  if (found === null) throw new Error('no control rendered');
  return found;
};

/**
 * Hands the field a control its own type already refuses. A toggle group's
 * props are a union over `type` that `FieldControlProps` cannot satisfy, so
 * the compiler stops the call before the component does. The runtime refusal
 * is the one that says why the group cannot be wrapped, and it is what a
 * caller writing JavaScript meets.
 */
const refused = (control: unknown): FieldControl => control as FieldControl;

afterEach(() => {
  for (const host of hosts.splice(0)) host.remove();
});

describe('Field', () => {
  it('names its control and describes it with the description', async () => {
    const host = inside();

    await render(Field, {
      target: host,
      props: {
        label: 'Nozzle diameter',
        description: 'In millimetres.',
        control: Input,
      },
    });

    await expect
      .element(page.elementLocator(controlOf(host)))
      .toHaveAccessibleName('Nozzle diameter');
    await expect
      .element(page.elementLocator(controlOf(host)))
      .toHaveAccessibleDescription('In millimetres.');
    await expectNoViolations(host);
  });

  it('reaches the control with the error and marks it invalid', async () => {
    const host = inside();

    await render(Field, {
      target: host,
      props: {
        label: 'Nozzle diameter',
        description: 'In millimetres.',
        error: 'Must be between 0.2 and 1.0.',
        control: Input,
        controlProps: { value: '1.6' },
      },
    });

    expect(controlOf(host).getAttribute('aria-invalid')).toBe('true');
    await expect
      .element(page.elementLocator(controlOf(host)))
      .toHaveAccessibleDescription(/Must be between 0\.2 and 1\.0\./);
  });

  it('paints the control, not the box around it, when the value is wrong', async () => {
    const good = inside();
    const bad = inside();

    await render(Field, {
      target: good,
      props: { label: 'Valid', control: Input, controlProps: { value: '0.4' } },
    });
    await render(Field, {
      target: bad,
      props: {
        label: 'Invalid',
        error: 'Required.',
        control: Input,
        controlProps: { value: '' },
      },
    });

    expect(getComputedStyle(controlOf(bad)).borderColor).not.toBe(
      getComputedStyle(controlOf(good)).borderColor,
    );
    // The red belongs to the control. The box around it keeps its own edge.
    const boxes = [good, bad].map((host) => {
      const box = host.querySelector('[data-slot="field"]');
      if (box === null) throw new Error('no field rendered');
      return getComputedStyle(box).borderColor;
    });
    expect(boxes[0]).toBe(boxes[1]);
  });

  it('keeps the description the caller wired beside its own', async () => {
    const host = inside();
    const hint = document.createElement('span');
    hint.id = 'firmware-hint';
    hint.textContent = 'Firmware-reported default.';
    host.append(hint);

    await render(Field, {
      target: host,
      props: {
        label: 'Nozzle diameter',
        description: 'In millimetres.',
        control: Input,
        controlProps: { 'aria-describedby': 'firmware-hint' },
      },
    });

    const described = controlOf(host).getAttribute('aria-describedby') ?? '';
    expect(described.split(' ')[0]).toBe('firmware-hint');
    await expect
      .element(page.elementLocator(controlOf(host)))
      .toHaveAccessibleDescription(/Firmware-reported default\./);
    await expect
      .element(page.elementLocator(controlOf(host)))
      .toHaveAccessibleDescription(/In millimetres\./);
  });

  it('gives two fields with the same label two different controls', async () => {
    const first = inside();
    const second = inside();
    const props = { label: 'Nozzle diameter', control: Input };

    await render(Field, { target: first, props });
    await render(Field, { target: second, props });

    const one = controlOf(first).id;
    const two = controlOf(second).id;
    expect(one.length).toBeGreaterThan(0);
    expect(one).not.toBe(two);
  });

  it('keeps the id the caller gave its control, and points the label at it', async () => {
    const host = inside();

    await render(Field, {
      target: host,
      props: {
        label: 'Nozzle diameter',
        control: Input,
        controlProps: { id: 'nozzle-diameter' },
      },
    });

    expect(controlOf(host).id).toBe('nozzle-diameter');
    expect(host.querySelector('label')?.getAttribute('for')).toBe(
      'nozzle-diameter',
    );
  });

  // The measurement the refusal below rests on. Without it the guard is a
  // belief, and the next reader deletes it.
  it('never lands focus on a roving-tabindex root, described or not', async () => {
    const host = inside();

    await render(RovingTabindexProbe, { target: host });

    const group = host.querySelector('[data-slot="radio-group"]');
    if (group === null) throw new Error('no group rendered');
    await expect
      .element(page.elementLocator(group))
      .toHaveAccessibleDescription('Firmware default is 0.4mm.');

    await userEvent.tab();

    expect(document.activeElement).not.toBe(group);
    expect(document.activeElement?.getAttribute('aria-describedby')).toBe(null);
  });

  it('refuses a composite whose root never takes focus', async () => {
    const host = inside();

    await expect(
      render(Field, {
        target: host,
        props: {
          label: 'Nozzle diameter',
          control: refused(ToggleGroup),
        },
      }),
    ).rejects.toThrow(/Field cannot wrap ToggleGroup/);
  });
});
