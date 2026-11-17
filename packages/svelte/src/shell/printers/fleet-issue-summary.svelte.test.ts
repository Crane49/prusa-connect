import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { t } from '../../i18n';
import FleetIssueSummary from './fleet-issue-summary.svelte';

describe('FleetIssueSummary', () => {
  it('draws a chip per severity present, worst first, and toggles the filter', async () => {
    const onSelectedChange = vi.fn();
    await render(FleetIssueSummary, {
      counts: { info: 1, warning: 0, error: 2 },
      selected: [],
      onSelectedChange,
    });

    const chips = page.getByRole('button').elements();
    expect(chips.map((chip) => chip.getAttribute('aria-label'))).toEqual([
      t('printers.issues.error', { count: 2 }),
      t('printers.issues.info', { count: 1 }),
    ]);
    expect(chips.map((chip) => chip.textContent.trim())).toEqual(['2', '1']);

    await userEvent.click(chips[0] as HTMLElement);
    expect(onSelectedChange).toHaveBeenCalledWith(['error']);
  });

  it('marks a chosen severity pressed and takes it off on a second click', async () => {
    const onSelectedChange = vi.fn();
    await render(FleetIssueSummary, {
      counts: { error: 2, warning: 0, info: 0 },
      selected: ['error'],
      onSelectedChange,
    });

    const chip = page.getByRole('button').element();
    expect(chip.getAttribute('aria-pressed')).toBe('true');
    await userEvent.click(chip);
    expect(onSelectedChange).toHaveBeenCalledWith([]);
  });

  it('draws nothing when nothing is wrong', async () => {
    await render(FleetIssueSummary, {
      counts: { error: 0, warning: 0, info: 0 },
      selected: [],
      onSelectedChange: vi.fn(),
    });

    expect(document.body.textContent.trim()).toBe('');
  });
});
