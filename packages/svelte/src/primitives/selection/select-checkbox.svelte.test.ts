import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import SelectionProbe from '../../test/selection-probe.svelte';

const box = (name: string) => page.getByRole('checkbox', { name });
const ticked = (name: string) =>
  box(name).element().getAttribute('aria-checked');

describe('SelectCheckbox and SelectAllCheckbox', () => {
  it('ticks the range from the last row pressed on a shift press', async () => {
    await render(SelectionProbe);

    await box('Select benchy.gcode').click();
    await userEvent.keyboard('{Shift>}');
    await box('Select vase.gcode').click();
    await userEvent.keyboard('{/Shift}');

    expect(ticked('Select benchy.gcode')).toBe('true');
    expect(ticked('Select cube.gcode')).toBe('true');
    expect(ticked('Select vase.gcode')).toBe('true');
    expect(ticked('Select hook.gcode')).toBe('false');
  });

  it('ticks one row on a plain press and on Space', async () => {
    await render(SelectionProbe);

    await box('Select benchy.gcode').click();
    await box('Select vase.gcode').click();
    expect(ticked('Select cube.gcode')).toBe('false');

    box('Select hook.gcode').element().focus();
    await userEvent.keyboard(' ');
    expect(ticked('Select hook.gcode')).toBe('true');
  });

  it('shows some rows ticked as mixed, and ticks them all on a press', async () => {
    await render(SelectionProbe, { props: { ticked: ['cube.gcode'] } });

    expect(ticked('Select every file')).toBe('mixed');
    await box('Select every file').click();
    expect(ticked('Select every file')).toBe('true');
    expect(ticked('Select hook.gcode')).toBe('true');

    await box('Select every file').click();
    expect(ticked('Select cube.gcode')).toBe('false');
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(SelectionProbe, {
      props: { ticked: ['cube.gcode'] },
    });

    await expectNoViolations(container);
  });
});
