import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../test/axe';
import { twoPartMesh } from '../test/project-meshes';
import ProjectObject3d from './project-object-3d.svelte';

const filaments = [
  { type: 'PLA', colour: '#E25C15' },
  { type: 'PETG', colour: '#2C7BE5' },
];

describe('ProjectObject3d', () => {
  it('draws the object and turns it from the keyboard', async () => {
    const onunavailable = vi.fn();
    await render(ProjectObject3d, {
      props: {
        mesh: twoPartMesh,
        filaments,
        label: 'Two rings in 3D',
        onunavailable,
      },
    });

    const view = page.getByRole('application', { name: 'Two rings in 3D' });
    await expect.element(view).toBeVisible();
    // The reset button shows once the viewer has drawn.
    const reset = page.getByRole('button', { name: 'Back to the first view' });
    await expect.element(reset).toBeVisible();
    expect(onunavailable).not.toHaveBeenCalled();

    view.element().focus();
    await userEvent.keyboard('{ArrowLeft}{ArrowUp}+-0');
    await userEvent.click(reset);
    await expectNoViolations(document.body);
  });

  it('sizes its canvas to the box it is drawn in', async () => {
    await render(ProjectObject3d, {
      props: { mesh: twoPartMesh, filaments, label: 'Two rings in 3D' },
    });
    await expect
      .element(page.getByRole('button', { name: 'Back to the first view' }))
      .toBeVisible();

    const canvas = document.querySelector('canvas');
    expect(canvas?.width).toBeGreaterThan(0);
    expect(canvas?.height).toBeGreaterThan(0);
  });

  it('draws a part whose filament is black', async () => {
    const onunavailable = vi.fn();
    await render(ProjectObject3d, {
      props: {
        mesh: twoPartMesh,
        filaments: [{ type: 'PLA', colour: '#000000' }],
        label: 'Two rings in 3D',
        onunavailable,
      },
    });
    await expect
      .element(page.getByRole('button', { name: 'Back to the first view' }))
      .toBeVisible();
    expect(onunavailable).not.toHaveBeenCalled();
  });
});
