<!--
  The PostgreSQL databases a rolled-back update could not restore, because
  they are kept outside the server. The operator restores each one from their
  own backup.
-->
<script lang="ts">
  import DatabaseIcon from '@lucide/svelte/icons/database';

  import { t } from '../../i18n';
  import { Alert, AlertDescription, AlertTitle } from '../../primitives';

  let {
    databases,
    class: className,
  }: { databases: readonly string[]; class?: string } = $props();
</script>

{#if databases.length > 0}
  <Alert variant="warning" class={className} data-part="external-restore">
    <DatabaseIcon aria-hidden="true" />
    <AlertTitle>{t('settingsMaintenance.externalRestore.title')}</AlertTitle>
    <AlertDescription>
      <span class="block">
        {t('settingsMaintenance.externalRestore.body')}
      </span>
      <ul class="mt-1 list-disc ps-5">
        {#each databases as database (database)}
          <li class="font-mono break-all">{database}</li>
        {/each}
      </ul>
    </AlertDescription>
  </Alert>
{/if}
