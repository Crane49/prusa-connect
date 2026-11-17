import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import TextareaProbe from '../../test/textarea-probe.svelte';
import Textarea from './textarea.svelte';
import type { TextareaProps } from './types';

const draw = async (props: TextareaProps = {}) => {
  const result = await render(Textarea, { props });
  const box = result.container.querySelector<HTMLTextAreaElement>(
    '[data-slot="textarea"]',
  );
  if (box === null) throw new Error('no textarea rendered');
  return { ...result, box };
};

describe('Textarea', () => {
  it('keeps what a person types', async () => {
    await draw({ 'aria-label': 'Printer notes' });

    const field = page.getByRole('textbox', { name: 'Printer notes' });
    await userEvent.type(field.element(), 'Nozzle 0.4 mm');

    await expect.element(field).toHaveValue('Nozzle 0.4 mm');
  });

  it('sends what a person types back to a bound value', async () => {
    const { container } = await render(TextareaProbe, {
      props: { value: 'Nozzle' },
    });

    const field = page.getByRole('textbox', { name: 'Printer notes' });
    await expect.element(field).toHaveValue('Nozzle');

    await userEvent.type(field.element(), ' 0.4 mm');

    expect(container.querySelector('output')?.textContent).toBe(
      'Nozzle 0.4 mm',
    );
  });

  it('takes no input and no focus while it is disabled', async () => {
    const { box } = await draw({
      'aria-label': 'Printer notes',
      disabled: true,
    });

    await userEvent.tab();
    expect(document.activeElement).not.toBe(box);

    box.focus();
    await userEvent.keyboard('Nozzle');
    expect(box.value).toBe('');
  });

  it('shows the placeholder until there is text, and drops it after', async () => {
    const { box } = await draw({
      'aria-label': 'Printer notes',
      placeholder: 'Printer notes',
    });

    expect(box.getAttribute('placeholder')).toBe('Printer notes');
    expect(box.value).toBe('');

    await userEvent.type(box, 'Bed levelled');
    expect(box.value).toBe('Bed levelled');
  });

  it('reports itself as invalid when the form says so', async () => {
    const { box } = await draw({
      'aria-label': 'Printer notes',
      'aria-invalid': 'true',
    });

    expect(box.getAttribute('aria-invalid')).toBe('true');
    await expect
      .element(page.getByRole('textbox', { name: 'Printer notes' }))
      .toBeInvalid();
  });

  it('grows with its content instead of showing a drag handle', async () => {
    const { box } = await draw({ 'aria-label': 'Printer notes' });
    const style = getComputedStyle(box);

    // `field-sizing: content` is what makes the box follow the text, and the
    // handle is off because the box is no longer the size a person set.
    expect(style.resize).toBe('none');
    expect(style.fieldSizing).toBe('content');

    const short = box.getBoundingClientRect().height;
    box.value = 'Line one\nLine two\nLine three\nLine four\nLine five';
    box.dispatchEvent(new Event('input', { bubbles: true }));
    expect(box.getBoundingClientRect().height).toBeGreaterThan(short);
  });

  it('is at least four lines tall when it is empty', async () => {
    const { box } = await draw({ 'aria-label': 'Printer notes' });

    // `min-h-16` is 4rem, the floor the design system sets.
    expect(getComputedStyle(box).minHeight).toBe('64px');
  });

  it('paints a ring on keyboard focus and none at rest', async () => {
    const { box } = await draw({ 'aria-label': 'Printer notes' });

    expect(getComputedStyle(box).boxShadow).toBe('none');

    await userEvent.tab();
    expect(document.activeElement).toBe(box);
    expect(box.matches(':focus-visible')).toBe(true);
    expect(getComputedStyle(box).boxShadow).not.toBe('none');
    // The outline is dropped so only the ring shows.
    expect(getComputedStyle(box).outlineStyle).toBe('none');
  });

  it('greys its text when disabled rather than fading the box', async () => {
    const live = await draw({ 'aria-label': 'Printer notes' });
    const liveColour = getComputedStyle(live.box).color;
    await live.unmount();

    const dead = await draw({ 'aria-label': 'Printer notes', disabled: true });
    const deadStyle = getComputedStyle(dead.box);

    expect(deadStyle.color).not.toBe(liveColour);
    expect(deadStyle.opacity).toBe('1');
    expect(deadStyle.pointerEvents).toBe('none');
    expect(deadStyle.backgroundColor).toBe('rgba(0, 0, 0, 0)');
  });

  it('keeps the class a caller adds beside its own', async () => {
    const { box } = await draw({
      'aria-label': 'Printer notes',
      class: 'max-w-sm',
    });

    expect(box.classList.contains('max-w-sm')).toBe(true);
    expect(box.classList.contains('rounded-sm')).toBe(true);
  });

  it('has no accessibility violation', async () => {
    const { container } = await draw({ 'aria-label': 'Printer notes' });

    await expectNoViolations(container);
  });
});
