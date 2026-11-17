<!--
  A bookmark to /settings/users is sent on to the People room, with its query
  and its fragment. The address it came from leaves no history entry.
-->
<script lang="ts">
  import { PATHS } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  $effect(() => {
    ctx.go(`${PATHS.settingsPeople}${ctx.search}${ctx.hash}`, {
      replace: true,
    });
  });
</script>
