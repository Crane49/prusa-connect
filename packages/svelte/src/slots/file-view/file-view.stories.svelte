<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import type { Component } from 'svelte';

  import DemoFileView from '../../test/demo-file-view.svelte';
  import { readyLoader } from '../../test/fakes';
  import ThrowingFileView from '../../test/throwing-file-view.svelte';
  import type { ModuleLoader } from '../loader.svelte';
  import { SlotRegistry } from '../registry.svelte';
  import FileView from './file-view.svelte';

  // The states of ADR-20261027-wvj rule 18, each over the same core part.

  const { Story } = defineMeta({
    title: 'Parts/File View',
    component: FileView,
  });

  const registryWith = (
    view: Component<{ fileRef: string }> | undefined,
  ): SlotRegistry => {
    const registry = new SlotRegistry();
    registry.declare('reader', {
      pages: [{ path: 'reader', title: 'Project reader', hidden: false }],
      file_view: [{ id: 'project', title: 'Project', file_types: ['.3mf'] }],
    });
    if (view !== undefined) {
      registry.registerSection('reader', 'file-view', 'project', view);
    }

    return registry;
  };

  const loadingLoader = {
    ready: false,
    activate: () => new Promise<void>(() => undefined),
  } as unknown as ModuleLoader;
</script>

{#snippet core()}
  <section class="flex flex-col gap-2">
    <h2 class="text-sm font-medium">Contents</h2>
    <p class="text-sm">What the server read from the file.</p>
  </section>
{/snippet}

<Story name="No Module Reads The Type" asChild>
  <FileView
    fileRef="local:part.gcode"
    fallback={core}
    registry={registryWith(DemoFileView)}
    loader={readyLoader()}
  />
</Story>

<Story name="Loading" asChild>
  <FileView
    fileRef="local:benchy.3mf"
    fallback={core}
    registry={registryWith(undefined)}
    loader={loadingLoader}
  />
</Story>

<Story name="Not Available" asChild>
  <FileView
    fileRef="local:benchy.3mf"
    fallback={core}
    registry={registryWith(undefined)}
    loader={readyLoader()}
  />
</Story>

<Story name="The View Throws" asChild>
  <FileView
    fileRef="local:benchy.3mf"
    fallback={core}
    registry={registryWith(ThrowingFileView)}
    loader={readyLoader()}
  />
</Story>

<Story name="Module View" asChild>
  <FileView
    fileRef="local:benchy.3mf"
    fallback={core}
    registry={registryWith(DemoFileView)}
    loader={readyLoader()}
  />
</Story>
