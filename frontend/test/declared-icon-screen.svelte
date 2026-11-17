<!-- A screen for the app's declared-model test: it reads the device-model
     vocabulary the way a printer screen does and draws one icon for a model
     the library has no drawing of. -->
<script lang="ts">
  import { PrinterIcon } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import type { AppContext } from '../src/lib/screens';

  let { ctx }: { ctx: AppContext } = $props();

  onMount(() => {
    void ctx.deviceModels.read();
  });
</script>

<PrinterIcon model="unknown" reported="bench one" />
