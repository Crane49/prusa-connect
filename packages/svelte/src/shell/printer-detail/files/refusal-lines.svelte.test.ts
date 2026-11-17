import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import RefusalLines from './refusal-lines.svelte';

const lines = (container: HTMLElement): string[] =>
  [...container.querySelectorAll('span, code')].map((line) => line.textContent);

describe('RefusalLines', () => {
  it('puts each part of the error on its own line', async () => {
    const { container } = await render(RefusalLines, {
      props: {
        title: 'The printer refused that change to its storage',
        reasons: [{ detail: 'no such file', names: 'a.gcode' }],
        action: 'List the folder first.',
        code: 'core.printer/FILE_CHANGE_REFUSED',
      },
    });

    expect(lines(container)).toEqual([
      'The printer refused that change to its storage',
      'no such file',
      'List the folder first.',
      'core.printer/FILE_CHANGE_REFUSED',
    ]);
  });

  // A part that is missing leaves no empty line and no stray full stop.
  it('leaves out a reason and an action the error does not have', async () => {
    const { container } = await render(RefusalLines, {
      props: {
        title: 'You may not do that',
        reasons: [],
        code: 'core.authz/FORBIDDEN',
      },
    });

    expect(lines(container)).toEqual([
      'You may not do that',
      'core.authz/FORBIDDEN',
    ]);
  });

  it('names the files beside each reason when the reasons differ', async () => {
    const { container } = await render(RefusalLines, {
      props: {
        title: 'The printer refused that change to its storage',
        reasons: [
          { detail: 'busy', names: 'a.gcode' },
          { detail: 'no such file', names: 'b.gcode' },
        ],
        code: 'core.printer/FILE_CHANGE_REFUSED',
      },
    });

    expect(lines(container)).toEqual([
      'The printer refused that change to its storage',
      'a.gcode · busy',
      'b.gcode · no such file',
      'core.printer/FILE_CHANGE_REFUSED',
    ]);
  });
});
