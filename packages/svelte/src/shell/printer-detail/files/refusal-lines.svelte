<!-- One error under a toast, one part per line, as a fault is shown elsewhere. -->
<script lang="ts">
  import { t } from '../../../i18n';
  import type { RefusalLinesProps } from './types';

  let { title, reasons, action, code }: RefusalLinesProps = $props();
</script>

<span class="block">{title}</span>
{#each reasons as reason (reason.detail)}
  <span class="block">
    {reasons.length === 1
      ? reason.detail
      : t('printerDetailFiles.delete.reasonFor', {
          names: reason.names,
          detail: reason.detail,
        })}
  </span>
{/each}
{#if action}<span class="block">{action}</span>{/if}
<code class="block text-xs">{code}</code>
