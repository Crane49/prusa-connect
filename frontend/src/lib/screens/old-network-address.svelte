<!--
  A bookmark to /settings/network is sent on to the Network tab of the Server
  room. The address it came from leaves no history entry.
-->
<script lang="ts">
  import { PATHS } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  $effect(() => {
    ctx.go(PATHS.settingsServerNetwork, { replace: true });
  });
</script>
