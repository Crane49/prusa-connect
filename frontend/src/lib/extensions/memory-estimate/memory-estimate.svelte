<!--
  How much memory one module will ask for, who chose it, and, when the enabled
  modules together would outgrow the pool, a warning. The module can still be
  enabled: the server warns and refuses nothing.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { Alert, AlertDescription } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import type { MemoryEstimate } from '../memory';

  let { estimate }: { estimate: MemoryEstimate } = $props();

  const t = translateFor(APP_NAMESPACE);
</script>

<div class="flex flex-col gap-1 text-sm" data-part="memory-estimate">
  <p>
    {t('app.settings.extensions.memory.estimate', { mib: estimate.mib })}
    <span class="text-xs text-muted-foreground" data-part="memory-source">
      {t(`app.settings.extensions.memory.source.${estimate.source}`)}
    </span>
  </p>
  {#if estimate.overPool && estimate.poolMiB !== undefined}
    <Alert variant="warning" data-part="memory-over-pool">
      <AlertDescription>
        {t('app.settings.extensions.memory.overPool', {
          committed: estimate.committedMiB,
          pool: estimate.poolMiB,
        })}
      </AlertDescription>
    </Alert>
  {/if}
</div>
