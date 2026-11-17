<!-- Compatibility address for bookmarks from before Server gained child tabs.
     It leads to Updates, or to the overview where core.upgrade.enabled is off
     and Updates is not drawn. -->
<script lang="ts">
  import { PATHS } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  $effect(() => {
    ctx.go(
      ctx.settings.upgrades
        ? PATHS.settingsServerUpdates
        : PATHS.settingsServerOverview,
      { replace: true },
    );
  });
</script>
