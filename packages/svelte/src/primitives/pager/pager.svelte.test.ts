import { describe, expect, it } from 'vitest';
import { page as browser, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import PagerProbe from '../../test/pager-probe.svelte';

/** The sentence between the size control and the two steps. */
const sentence = (): HTMLElement | null =>
  document.querySelector('p[aria-live="polite"]');

const said = (): string =>
  (sentence()?.textContent ?? '').replace(/\s+/g, ' ').trim();

const option = (label: string): HTMLElement | null =>
  document.querySelector(`[data-slot="select-item"][data-label="${label}"]`);

describe('Pager', () => {
  it('says where the reader is and how much is in front of them', async () => {
    await render(PagerProbe, { props: { unit: 'jobs' } });

    // A page number alone leaves the size of the list to arithmetic, so both
    // numbers are in the sentence.
    expect(said()).toBe('Page 1 of 3 · showing 25 of 61 jobs');

    await browser.getByRole('button', { name: 'Next' }).click();
    await browser.getByRole('button', { name: 'Next' }).click();

    // The last page holds what is left, not a full page.
    await expect.poll(said).toBe('Page 3 of 3 · showing 11 of 61 jobs');
  });

  it('names no unit for a listing that gives none', async () => {
    await render(PagerProbe, {});

    expect(said()).toBe('Page 1 of 3 · showing 25 of 61');
  });

  it('stops at both ends of the list', async () => {
    await render(PagerProbe, { props: { total: 30 } });

    const previous = browser.getByRole('button', { name: 'Previous' });
    const next = browser.getByRole('button', { name: 'Next' });

    await expect.element(previous).toBeDisabled();
    await expect.element(next).not.toBeDisabled();

    await next.click();

    await expect.poll(said).toBe('Page 2 of 2 · showing 5 of 30');
    await expect.element(next).toBeDisabled();
    await expect.element(previous).not.toBeDisabled();

    await previous.click();

    await expect.poll(said).toBe('Page 1 of 2 · showing 25 of 30');
  });

  it('announces the move, because the rows change silently', async () => {
    await render(PagerProbe, {});

    expect(sentence()?.getAttribute('aria-live')).toBe('polite');
  });

  it('changes the size from the control the label names', async () => {
    await render(PagerProbe, {});

    const size = browser.getByLabelText('Per page');

    await expect.element(size).toHaveTextContent('25');

    size.element().focus();
    await userEvent.keyboard('{Enter}');
    await expect.poll(() => option('50')).not.toBe(null);

    // 50 is the step after the one the listing holds.
    await userEvent.keyboard('{ArrowDown}');
    await userEvent.keyboard('{Enter}');

    // A bigger page holds the whole list, so the steps are done.
    await expect.poll(said).toBe('Page 1 of 2 · showing 50 of 61');
    await expect.element(size).toHaveTextContent('50');
  });

  it('gives a listing its own count in place of the sentence', async () => {
    await render(PagerProbe, { props: { withCount: true } });

    await expect
      .element(browser.getByTestId('own-count'))
      .toHaveTextContent('Four of 61 match');
    // One live region, not two: the caller's count carries its own.
    expect(document.querySelectorAll('[aria-live="polite"]')).toHaveLength(1);
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(PagerProbe, { props: { unit: 'jobs' } });

    await expectNoViolations(container);
  });
});
