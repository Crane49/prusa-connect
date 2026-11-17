import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { t } from '../../i18n';
import PlainFileView from '../../test/plain-file-view.svelte';
import ThrowingFileView from '../../test/throwing-file-view.svelte';
import { readyLoader } from '../../test/fakes';
import type { ModuleLoader } from '../loader.svelte';
import { SlotRegistry } from '../registry.svelte';
import FileView from './file-view.svelte';

// The four states of ADR-20261027-wvj rule 18, and the file_types filter of
// rule 17.

const fallback = createRawSnippet(() => ({
  render: () => '<p data-testid="core-contents">what the server read</p>',
}));

/** A loader whose bundles never finish loading. */
const loadingLoader = (): ModuleLoader =>
  ({
    ready: false,
    activate: () => new Promise<void>(() => undefined),
  }) as unknown as ModuleLoader;

const declaring = (): SlotRegistry => {
  const registry = new SlotRegistry();
  registry.declare('reader', {
    pages: [{ path: 'reader', title: 'Demo reader', hidden: false }],
    file_view: [{ id: 'project', title: 'Project', file_types: ['.demo'] }],
  });

  return registry;
};

const core = () => page.getByTestId('core-contents');
const note = () =>
  page.getByText(t('fileView.unavailable', { module: 'Demo reader' }));

describe('FileView', () => {
  it('draws the core part when no module draws this file type', async () => {
    await render(FileView, {
      props: {
        fileRef: 'bench:box.demo',
        fallback,
        registry: new SlotRegistry(),
        loader: readyLoader(),
      },
    });

    await expect.element(core()).toBeInTheDocument();
    await expect.element(note()).not.toBeInTheDocument();
  });

  it('draws the core part for a file of a type the view does not name', async () => {
    const registry = declaring();
    registry.registerSection('reader', 'file-view', 'project', PlainFileView);

    await render(FileView, {
      props: {
        fileRef: 'bench:part.gcode',
        fallback,
        registry,
        loader: readyLoader(),
      },
    });

    await expect.element(core()).toBeInTheDocument();
    await expect
      .element(page.getByTestId('plain-file-view'))
      .not.toBeInTheDocument();
  });

  it('holds the place while the bundle is still loading', async () => {
    await render(FileView, {
      props: {
        fileRef: 'bench:box.demo',
        fallback,
        registry: declaring(),
        loader: loadingLoader(),
      },
    });

    const placeholder = page.getByText(t('fileView.loading'));
    await expect.element(placeholder).toBeInTheDocument();
    expect(
      document
        .querySelector('[data-file-view="loading"]')
        ?.getAttribute('aria-busy'),
    ).toBe('true');
    await expect.element(core()).not.toBeInTheDocument();
  });

  it('says the view is not available when the bundle loaded and did not register it', async () => {
    await render(FileView, {
      props: {
        fileRef: 'bench:box.demo',
        fallback,
        registry: declaring(),
        loader: readyLoader(),
      },
    });

    await expect.element(note()).toBeInTheDocument();
    await expect.element(core()).toBeInTheDocument();
  });

  it('says the view is not available when the module was refused', async () => {
    const registry = declaring();
    registry.refuse({ namespace: 'reader', reason: 'threw', detail: 'boom' });

    await render(FileView, {
      props: {
        fileRef: 'bench:box.demo',
        fallback,
        registry,
        loader: readyLoader(),
      },
    });

    await expect.element(note()).toBeInTheDocument();
    await expect.element(core()).toBeInTheDocument();
  });

  it('draws the module view in place of the core part', async () => {
    const registry = declaring();
    registry.registerSection('reader', 'file-view', 'project', PlainFileView);

    await render(FileView, {
      props: {
        fileRef: 'bench:BOX.DEMO',
        fallback,
        registry,
        loader: readyLoader(),
      },
    });

    await expect
      .element(page.getByTestId('plain-file-view'))
      .toHaveTextContent('module view of bench:BOX.DEMO');
    await expect
      .element(page.getByRole('heading', { name: 'Project' }))
      .toBeInTheDocument();
    await expect.element(core()).not.toBeInTheDocument();
  });

  it('draws the core part with the note when the module view throws', async () => {
    const registry = declaring();
    registry.registerSection(
      'reader',
      'file-view',
      'project',
      ThrowingFileView,
    );

    await render(FileView, {
      props: {
        fileRef: 'bench:box.demo',
        fallback,
        registry,
        loader: readyLoader(),
      },
    });

    await expect.element(note()).toBeInTheDocument();
    await expect.element(core()).toBeInTheDocument();
    expect(registry.refusals()).toEqual([
      {
        namespace: 'reader',
        reason: 'threw',
        detail: 'this view is broken: bench:box.demo',
      },
    ]);
  });

  it('starts clean on another file after a view threw', async () => {
    const registry = declaring();
    registry.registerSection(
      'reader',
      'file-view',
      'project',
      ThrowingFileView,
    );

    const drawn = await render(FileView, {
      props: {
        fileRef: 'bench:box.demo',
        fallback,
        registry,
        loader: readyLoader(),
      },
    });
    await expect.element(note()).toBeInTheDocument();

    await drawn.rerender({ fileRef: 'bench:part.gcode' });

    await expect.element(note()).not.toBeInTheDocument();
    await expect.element(core()).toBeInTheDocument();
  });
});
