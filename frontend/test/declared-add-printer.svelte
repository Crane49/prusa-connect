<!-- The add-printer page under the lookup the application hands the printer
     icons, for the test of the spelling the page passes them. -->
<script lang="ts">
  import { setDeclaredModels } from '@prusa/connect-ui/primitives';

  import type { AppContext } from '../src/lib/screens';
  import AddPrinter from '../src/lib/screens/add-printer.svelte';

  let { ctx }: { ctx: AppContext } = $props();

  setDeclaredModels((reported) => ctx.deviceModels.declared(reported));
</script>

<AddPrinter {ctx} />
