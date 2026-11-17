<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Badge from '../badge/badge.svelte';
  import Button from '../button/button.svelte';
  import PrinterIcon from '../printer-icon/printer-icon.svelte';
  import DetailHeader from './detail-header.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Detail Header',
    component: DetailHeader,
    args: { title: 'MK4S' },
  });
</script>

{#snippet media()}
  <PrinterIcon model="mk4s" class="size-32" />
{/snippet}

{#snippet titleAside()}
  <Badge variant="stateActive">Printing</Badge>
{/snippet}

{#snippet meta()}
  <span class="text-sm text-muted-foreground">Print Farm Rack 3</span>
{/snippet}

{#snippet actions()}
  <Button size="sm" variant="secondary">Rename</Button>
  <Button size="sm">Print</Button>
{/snippet}

<!-- The whole subject block: a picture, the name with the state beside it, the
     lines under them, the acts. -->
<Story name="Default" args={{ media, titleAside, meta, actions }} />

<!-- A subject with nothing offered on it. -->
<Story name="Without Actions" args={{ media, titleAside, meta }} />

<!-- A name every character of which belongs on screen. It wraps; it never
     truncates. -->
<Story
  name="A Name That Does Not Fit"
  args={{
    media,
    titleAside,
    meta,
    actions,
    title: 'Prusa CORE One, bench 4, left-hand column, night shift',
  }}
/>

<!-- The header's own shape while it waits: a picture, a title and two lines. -->
<Story name="Loading" args={{ loading: true }} />
