<!--
  One screen under a slot context, which is what the composition root puts
  there. A screen reads the registry and the loader out of the context, so a
  test that renders the screen on its own would read an empty one.
-->
<script lang="ts">
  import type { Component } from 'svelte';

  import {
    type ModuleLoader,
    setSlotContext,
    type SlotRegistry,
  } from '@prusa/connect-ui/slots';

  import type { AppContext } from '../lib/screens';

  interface Props {
    registry: SlotRegistry;
    loader?: ModuleLoader | undefined;
    screen: Component<{ ctx: AppContext }>;
    ctx: AppContext;
  }

  let { registry, loader, screen: Screen, ctx }: Props = $props();

  // Getters, because both arrive as props: reading them here would capture
  // the value the first render was given.
  setSlotContext({
    get registry() {
      return registry;
    },
    get loader() {
      return loader;
    },
  });
</script>

<Screen {ctx} />
