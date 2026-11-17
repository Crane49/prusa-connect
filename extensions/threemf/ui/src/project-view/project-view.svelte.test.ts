import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../test/axe';
import {
  bambuPlates,
  generic,
  multibed,
  notAProject,
  orcaPlates,
  plateThumbnail,
} from '../test/project-fixtures';
import {
  colourChanges,
  gcodeOnly,
  indxPainted,
  layerRanges,
  mmuObjects,
} from '../test/project-fixtures-multimaterial';
import { twoPartMesh } from '../test/project-meshes';
import ProjectView from './project-view.svelte';

const copiesOf = (object: number): HTMLElement[] => [
  ...document.querySelectorAll<HTMLElement>(
    `[data-copy^="${String(object)}:"]`,
  ),
];

const PICTURE = 'The picture the slicer saved of the project';

describe('ProjectView', () => {
  it('shows the project picture unless the page already does', async () => {
    const view = await render(ProjectView, {
      props: { project: multibed, thumbnailUrl: plateThumbnail },
    });
    await expect
      .element(page.getByRole('img', { name: PICTURE }))
      .toBeVisible();

    await view.rerender({
      project: multibed,
      thumbnailUrl: plateThumbnail,
      picture: false,
    });
    await expect
      .element(page.getByRole('img', { name: PICTURE }))
      .not.toBeInTheDocument();
    await expect
      .element(page.getByRole('heading', { name: 'Beds and objects' }))
      .toBeVisible();
  });

  it('says it is reading while the project is on its way', async () => {
    await render(ProjectView, { props: { loading: true } });

    await expect
      .element(page.getByRole('status', { name: 'Reading the project' }))
      .toBeVisible();
  });

  it('shows the code a project was refused with', async () => {
    await render(ProjectView, { props: { fault: notAProject } });

    await expect
      .element(
        page.getByRole('heading', { name: 'The project could not be read' }),
      )
      .toBeVisible();
    await expect.element(page.getByText('threemf/NOT_A_PROJECT')).toBeVisible();
  });

  it('leads with the overview, then the beds and objects, then the settings', async () => {
    await render(ProjectView, {
      props: { project: multibed, thumbnailUrl: plateThumbnail },
    });

    const headings = [
      ...document.querySelectorAll('[data-slot="project-view"] h2'),
    ].map((heading) => heading.textContent.trim());
    expect(headings).toEqual(['real296', 'Beds and objects', 'Settings']);
    await expect.element(page.getByText('PrusaSlicer 2.9.6')).toBeVisible();
    await expect
      .element(page.getByRole('img', { name: /saved of the project/ }))
      .toBeVisible();
    expect(page.getByRole('tab').all()).toHaveLength(0);
    // The plan opens on one bed, not on every bed at once.
    await expect
      .element(page.getByRole('group', { name: 'Plan of Bed 1, to scale' }))
      .toBeVisible();
    await expectNoViolations(document.body);
  });

  it('switches the bed the plan and the list show', async () => {
    await render(ProjectView, { props: { project: multibed } });

    await userEvent.click(page.getByRole('button', { name: /^Bed 2/ }));

    await expect
      .element(page.getByRole('group', { name: 'Plan of Bed 2, to scale' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: /^Bed 2/ }))
      .toHaveAttribute('aria-pressed', 'true');
  });

  it('ticks an object in the list and shows it on the plan and in the bar', async () => {
    await render(ProjectView, {
      props: { project: multibed, onselect: () => undefined },
    });
    await userEvent.click(page.getByRole('button', { name: /^All beds/ }));

    await userEvent.click(page.getByRole('checkbox', { name: 'Tick CubeB' }));

    expect(copiesOf(1).length).toBeGreaterThan(0);
    expect(copiesOf(1).every((copy) => copy.dataset.selected === 'true')).toBe(
      true,
    );
    await expect
      .element(page.getByText(/2 copies of 1 object ticked/))
      .toBeVisible();

    await userEvent.click(
      page.getByRole('button', { name: 'Clear the selection' }),
    );
    expect(copiesOf(1).every((copy) => copy.dataset.selected === 'false')).toBe(
      true,
    );
  });

  // A page that does nothing with ticked copies, such as a file's page, has
  // nothing to tick: a press on a copy opens its object.
  it('opens an object from the plan where nothing is ticked', async () => {
    await render(ProjectView, { props: { project: multibed } });
    await userEvent.click(page.getByRole('button', { name: /^All beds/ }));

    expect(page.getByRole('checkbox').all()).toHaveLength(0);
    await userEvent.click(
      page.getByRole('button', { name: /^CubeB, copy 1 of 2/ }),
    );

    await expect
      .element(page.getByRole('dialog').getByRole('heading', { name: 'CubeB' }))
      .toBeVisible();
    expect(page.getByRole('dialog').getByRole('checkbox').all()).toHaveLength(
      0,
    );
  });

  it('lights the copies of an object while the pointer is on its row', async () => {
    await render(ProjectView, { props: { project: multibed } });
    await userEvent.click(page.getByRole('button', { name: /^All beds/ }));

    await userEvent.hover(
      page.getByRole('button', { name: 'CubeB 2×', exact: true }),
    );

    expect(copiesOf(1).every((copy) => copy.dataset.lit === 'true')).toBe(true);
    expect(copiesOf(0).every((copy) => copy.dataset.lit === 'false')).toBe(
      true,
    );
  });

  it('marks the row of a copy pressed on the plan', async () => {
    await render(ProjectView, {
      props: { project: multibed, onselect: () => undefined },
    });
    await userEvent.click(page.getByRole('button', { name: /^All beds/ }));

    await userEvent.click(
      page.getByRole('button', { name: /^CubeB, copy 1 of 2/ }),
    );

    const row = document.querySelector('[data-object="1"]');
    expect(row?.getAttribute('data-lit')).toBe('true');
    await expect
      .element(page.getByRole('checkbox', { name: 'Tick CubeB' }))
      .toHaveAttribute('data-state', 'indeterminate');
  });

  it('opens an object in its panel with its parts and copies', async () => {
    await render(ProjectView, {
      props: { project: mmuObjects, onselect: () => undefined },
    });

    await userEvent.click(
      page.getByRole('button', { name: 'MMU_5COLOR_TEST.stl', exact: true }),
    );

    const panel = page.getByRole('dialog');
    await expect
      .element(panel.getByRole('heading', { name: 'MMU_5COLOR_TEST.stl' }))
      .toBeVisible();
    await expect
      .element(panel.getByRole('img', { name: /seen from above/ }))
      .toBeVisible();
    expect(
      panel.getByRole('list', { name: 'Extruder of the walls' }).all(),
    ).toHaveLength(5);
    await expect
      .element(panel.getByRole('checkbox', { name: /Copy 1/ }))
      .toBeVisible();
    await expectNoViolations(document.body);
  });

  it('shows the filaments of the project in their colours', async () => {
    await render(ProjectView, { props: { project: mmuObjects } });

    const filaments = page.getByRole('list', { name: 'Filaments' });
    await expect.element(filaments).toBeVisible();
    expect(filaments.getByRole('listitem').all()).toHaveLength(5);
    await expect
      .element(filaments.getByRole('img', { name: '#DB5182' }))
      .toBeVisible();
  });

  it('says in the panel which tools painted an object', async () => {
    await render(ProjectView, { props: { project: indxPainted } });

    await userEvent.click(
      page.getByRole('button', {
        name: indxPainted.objects[0]?.name ?? '',
        exact: true,
      }),
    );

    const panel = page.getByRole('dialog');
    await expect
      .element(panel.getByText('Painted colours').first())
      .toBeVisible();
    await expect
      .element(panel.getByRole('list', { name: 'Painted with' }))
      .toBeVisible();
  });

  it('lists the layer ranges of an object in its panel', async () => {
    await render(ProjectView, { props: { project: layerRanges } });

    await userEvent.click(
      page.getByRole('button', {
        name:
          layerRanges.objects.find((object) =>
            object.name?.startsWith('Long Left Side Box'),
          )?.name ?? '',
        exact: true,
      }),
    );

    await expect
      .element(
        page.getByRole('dialog').getByRole('heading', { name: 'Layer ranges' }),
      )
      .toBeVisible();
  });

  it('keeps what could not be read in one line that opens', async () => {
    await render(ProjectView, { props: { project: bambuPlates } });

    await expect.element(page.getByText('Not supported')).toBeVisible();
    const line = page.getByRole('button', {
      name: /could not be read in full/,
    });
    await expect.element(line).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(line);
    await expect
      .element(page.getByText(/is not a slicer this server supports/))
      .toBeVisible();
  });

  it('names the OrcaSlicer plates and shows their pictures', async () => {
    await render(ProjectView, {
      props: { project: orcaPlates, thumbnailUrl: plateThumbnail },
    });

    await expect
      .element(page.getByRole('button', { name: /^Bed 2, Lid/ }))
      .toBeVisible();
  });

  it('says a plain 3MF holds no settings', async () => {
    await render(ProjectView, { props: { project: generic } });

    await expect
      .element(page.getByText(/The file holds no slicer settings/))
      .toBeVisible();
    await expect
      .element(page.getByText('UltiMaker Cura 5.7.2, plain 3MF'))
      .toBeVisible();
  });

  it('lists the filament changes of the bed the plan shows', async () => {
    await render(ProjectView, { props: { project: colourChanges } });

    await expect
      .element(page.getByRole('heading', { name: 'Changes by height' }))
      .toBeVisible();
    expect(page.getByText('Filament change').all()).toHaveLength(6);
    await expect
      .element(page.getByRole('img', { name: '#8B6914' }))
      .toBeVisible();
    await expectNoViolations(document.body);
  });

  it('says a file of G-code alone prints as it is', async () => {
    await render(ProjectView, { props: { project: gcodeOnly } });

    await expect
      .element(page.getByRole('heading', { name: 'Sliced G-code only' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('heading', { name: 'Beds and objects' }))
      .not.toBeInTheDocument();
    await expect.element(page.getByText('G-code stored')).toBeVisible();
    await expect
      .element(page.getByText('The file holds G-code and no objects.'))
      .not.toBeInTheDocument();
    await expectNoViolations(document.body);
  });

  it('asks for the shape of an object when it is opened, and draws it in 3D', async () => {
    const loadMesh = vi.fn();
    const index = mmuObjects.objects.findIndex(
      (object) => object.name === 'MMU_5COLOR_TEST.stl',
    );
    await render(ProjectView, {
      props: {
        project: mmuObjects,
        loadMesh,
        mesh: (object: number) =>
          object === index ? { mesh: twoPartMesh } : undefined,
      },
    });

    await userEvent.click(
      page.getByRole('button', { name: 'MMU_5COLOR_TEST.stl', exact: true }),
    );

    expect(loadMesh).toHaveBeenCalledWith(index);
    const panel = page.getByRole('dialog');
    await expect
      .element(panel.getByRole('application', { name: /in 3D/ }))
      .toBeVisible();

    await userEvent.click(panel.getByRole('radio', { name: 'From above' }));
    await expect
      .element(panel.getByRole('img', { name: /seen from above/ }))
      .toBeVisible();
  });

  it('shows an object from above while its shape could not be read', async () => {
    await render(ProjectView, {
      props: {
        project: mmuObjects,
        loadMesh: () => undefined,
        mesh: () => ({
          fault: {
            code: 'threemf/NO_OBJECT',
            status: 404,
            params: {},
            message: undefined,
            traceId: undefined,
          },
        }),
      },
    });

    await userEvent.click(
      page.getByRole('button', { name: 'MMU_5COLOR_TEST.stl', exact: true }),
    );

    const panel = page.getByRole('dialog');
    await expect
      .element(panel.getByRole('img', { name: /seen from above/ }))
      .toBeVisible();
    await expect
      .element(panel.getByText(/could not be read, so it is shown from above/))
      .toBeVisible();
  });

  it('says the shape is on its way', async () => {
    await render(ProjectView, {
      props: {
        project: mmuObjects,
        loadMesh: () => undefined,
        mesh: () => undefined,
      },
    });

    await userEvent.click(
      page.getByRole('button', { name: 'MMU_5COLOR_TEST.stl', exact: true }),
    );

    await expect
      .element(page.getByRole('status', { name: "Reading the object's shape" }))
      .toBeVisible();
  });
});
