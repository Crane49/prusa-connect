<!--
  The real app at one address, over the story core of `api.ts`. The core is
  installed while this component initialises, before `App` asks for anything,
  and a new fleet size draws a new `App` over a new core. The size is the
  toolbar's `printers` global unless `size` fixes one. Leaving the story puts
  the real `fetch` and the Storybook address back.
-->
<script module lang="ts">
  // Read once, as Storybook loads this file, so an `AppAt` that mounts while
  // another still holds an app address puts back the Storybook one.
  const storybook = location.href;
</script>

<script lang="ts">
  import { onDestroy } from 'svelte';

  import { shownFleet } from '../../.storybook/fleet';
  import App from '../../src/app.svelte';
  import { type FleetSize, installApi, type Overrides } from './api';

  interface Props {
    path: string;
    size?: FleetSize;
    overrides?: Overrides;
  }

  let { path, size, overrides = {} }: Props = $props();

  const toolbar = shownFleet();
  const fleet = $derived(size ?? toolbar());

  let restore = (): void => undefined;

  // Called from the key below, so it runs before each `App` is created.
  const serve = (fleet: FleetSize, at: string): string => {
    restore = installApi(fleet, overrides);
    history.replaceState(null, '', at);

    return `${fleet} ${at}`;
  };

  onDestroy(() => {
    restore();
    history.replaceState(null, '', storybook);
  });
</script>

{#key serve(fleet, path)}
  <App />
{/key}
