// The project view of a file in a storage: it reads the file through the
// store once, draws each state, starts afresh on another file, and tells a
// page that embeds it what is ticked.
import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { Projects } from '../projects.svelte';
import { holdProjects } from '../session';
import { ANSWERS, fakeService } from '../test/projects';
import ProjectFile from './project-file.svelte';

const held = () => {
  const service = fakeService(ANSWERS);
  holdProjects(new Projects(service), new AbortController().signal);

  return service;
};

describe('ProjectFile', () => {
  it('reads a file once for two views of it', async () => {
    const service = held();
    await render(ProjectFile, {
      storage: 'local',
      path: 'projects/multibed.3mf',
    });
    await render(ProjectFile, {
      storage: 'local',
      path: 'projects/multibed.3mf',
    });

    await expect
      .element(page.getByRole('heading', { name: 'Beds and objects' }).first())
      .toBeVisible();
    expect(service.asked).toEqual(['local:projects/multibed.3mf']);
  });

  it('says it is reading while the module has not answered', async () => {
    held();
    await render(ProjectFile, {
      storage: 'local',
      path: 'projects/loading.3mf',
    });

    await expect
      .element(page.getByRole('status', { name: 'Reading the project' }))
      .toBeVisible();
  });

  it('says why a file could not be read, with its code', async () => {
    held();
    await render(ProjectFile, {
      storage: 'local',
      path: 'projects/hidden.3mf',
    });

    await expect
      .element(page.getByText('The storage does not hold that file'))
      .toBeVisible();
    await expect.element(page.getByText('threemf/NO_FILE')).toBeVisible();
  });

  it('tells the page what is ticked, one selection per bed', async () => {
    held();
    const onselect = vi.fn();
    await render(ProjectFile, {
      storage: 'local',
      path: 'projects/multibed.3mf',
      onselect,
    });
    await userEvent.click(page.getByRole('button', { name: /^All beds/ }));

    await userEvent.click(page.getByRole('checkbox', { name: 'Tick CubeB' }));

    expect(onselect).toHaveBeenCalled();
    const selections = onselect.mock.lastCall?.[0] as readonly {
      bed: { setup: number; bed: number };
      items: readonly number[];
    }[];
    expect(selections.length).toBeGreaterThan(0);
    expect(selections.flatMap((one) => one.items).length).toBe(2);

    await userEvent.click(
      page.getByRole('button', { name: 'Clear the selection' }),
    );
    expect(onselect).toHaveBeenLastCalledWith([]);
  });

  it('starts afresh on another file', async () => {
    const service = held();
    const view = await render(ProjectFile, {
      storage: 'local',
      path: 'projects/multibed.3mf',
    });
    await expect
      .element(page.getByRole('heading', { name: 'Beds and objects' }))
      .toBeVisible();

    await view.rerender({ storage: 'local', path: 'projects/damaged.3mf' });

    await expect.element(page.getByText('threemf/NOT_A_PROJECT')).toBeVisible();
    expect(service.asked).toEqual([
      'local:projects/multibed.3mf',
      'local:projects/damaged.3mf',
    ]);
  });
});
