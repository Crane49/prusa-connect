// The setup registers what module.yaml declares, and the registered view
// reads the project through this module's own route.
import type { Component } from 'svelte';
import { render } from 'vitest-browser-svelte';
import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';

import setup from './index';
import { multibed } from './test/project-fixtures';
import { MOUNT, stubHost } from './test/host';
import type { ProjectFileProps } from './project-file';

describe('the 3MF module', () => {
  it('registers the project file view and the project-view component', async () => {
    const stub = stubHost();
    await setup(stub.host);

    expect(Object.keys(stub.fileViews)).toEqual(['project']);
    expect(Object.keys(stub.components)).toEqual(['project-view']);
    stub.leave();
  });

  it('draws the project a file names through the module route', async () => {
    const stub = stubHost({
      [`GET ${MOUNT}/project`]: { status: 200, body: multibed },
    });
    await setup(stub.host);
    const view = stub.fileViews.project as unknown as Component<{
      fileRef: string;
    }>;

    await render(view, { fileRef: 'local:projects/box.3mf' });

    await expect
      .element(page.getByRole('heading', { name: 'Beds and objects' }))
      .toBeVisible();
    // The file's page shows the picture in its header, so the view does not.
    await expect
      .element(
        page.getByRole('img', {
          name: 'The picture the slicer saved of the project',
        }),
      )
      .not.toBeInTheDocument();
    const call = stub.calls.find((made) => made.path === `${MOUNT}/project`);
    expect(call?.query.get('storage')).toBe('local');
    expect(call?.query.get('path')).toBe('projects/box.3mf');
    stub.leave();
  });

  it('says why when the module refuses the file', async () => {
    const stub = stubHost({
      [`GET ${MOUNT}/project`]: {
        status: 422,
        body: {
          type: 'about:blank',
          status: 422,
          code: 'threemf/NOT_A_PROJECT',
          title: 'not a project',
          detail: 'the file is not a 3MF project',
          params: {},
          retryable: false,
        },
      },
    });
    await setup(stub.host);
    const view = stub.components[
      'project-view'
    ] as unknown as Component<ProjectFileProps>;

    await render(view, { storage: 'local', path: 'notes.3mf' });

    await expect
      .element(
        page.getByText('That file is not a 3MF project the module can read'),
      )
      .toBeVisible();
    await expect.element(page.getByText('threemf/NOT_A_PROJECT')).toBeVisible();
    stub.leave();
  });
});
