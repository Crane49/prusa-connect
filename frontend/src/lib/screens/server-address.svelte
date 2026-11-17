<!-- The Server room opens on Overview and leaves no extra history entry. -->
<script lang="ts">
  import { PATHS } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  $effect(() => {
    ctx.go(PATHS.settingsServerOverview, { replace: true });
  });
</script>
