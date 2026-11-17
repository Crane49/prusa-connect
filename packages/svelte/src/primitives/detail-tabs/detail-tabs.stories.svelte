<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import DetailTabs from './detail-tabs.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Detail Tabs',
    component: DetailTabs,
  });
</script>

{#snippet body(text: string)}
  <p class="text-sm text-muted-foreground">{text}</p>
{/snippet}

{#snippet statusPanel()}{@render body('The printer, right now.')}{/snippet}
{#snippet filesPanel()}{@render body('12 files on USB.')}{/snippet}

<Story name="Default" asChild>
  <DetailTabs
    active="status"
    onChange={() => undefined}
    tabs={[
      { id: 'status', label: 'Status', panel: statusPanel },
      { id: 'files', label: 'Files', panel: filesPanel },
    ]}
  />
</Story>

<!-- A tab whose panel is on its way. The frame ships first; the panel arrives
     later, and nothing in between is broken. -->
<Story name="A Tab With No Panel Yet" asChild>
  <DetailTabs
    active="info"
    onChange={() => undefined}
    tabs={[
      { id: 'status', label: 'Status', panel: statusPanel },
      { id: 'info', label: 'Info' },
    ]}
  />
</Story>

<!-- More tabs than the room holds: the strip scrolls and the edges fade. -->
<Story name="More Tabs Than Room" asChild>
  <div class="w-72">
    <DetailTabs
      active="status"
      onChange={() => undefined}
      tabs={[
        { id: 'status', label: 'Status', panel: statusPanel },
        { id: 'controls', label: 'Controls' },
        { id: 'files', label: 'Files', panel: filesPanel },
        { id: 'info', label: 'Info' },
        { id: 'commands', label: 'Commands' },
      ]}
    />
  </div>
</Story>

{#snippet notBuilt()}
  <p class="text-sm text-muted-foreground">
    This build does not draw this tab yet.
  </p>
{/snippet}

<!-- A tab this build draws no panel for at all. A skeleton here would pulse
     for good and read as a broken page. -->
<Story name="A Tab This Build Does Not Draw" asChild>
  <DetailTabs
    active="info"
    onChange={() => undefined}
    fallback={notBuilt}
    tabs={[
      { id: 'status', label: 'Status', panel: statusPanel },
      { id: 'info', label: 'Info' },
    ]}
  />
</Story>

<Story name="With a count" asChild>
  <DetailTabs
    active="status"
    onChange={() => undefined}
    tabs={[
      { id: 'status', label: 'Status', panel: statusPanel },
      { id: 'jobs', label: 'Jobs', count: 3, panel: filesPanel },
    ]}
  />
</Story>
