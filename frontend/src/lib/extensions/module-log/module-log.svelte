<!--
  What a module wrote to standard error lately, oldest first. Only a person
  who may configure the module reads it.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { CodeBlock, FaultAlert } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import { describeExtensionFault } from '../fault';
  import type { ResourceLog } from '../../stores/module-operations.svelte';
  import { logText } from './log-text';

  let {
    log,
    refused = false,
    missing = false,
    fault,
  }: {
    /** Undefined until the first read answers. */
    log?: ResourceLog | undefined;
    /** The server answered 403: the person may not read the log. */
    refused?: boolean;
    /** The server no longer knows the module. */
    missing?: boolean;
    /** What the last read failed with; shown only while no reading is on screen. */
    fault?: unknown;
  } = $props();

  const t = translateFor(APP_NAMESPACE);
  const KEY = 'app.settings.extensions.operations';
</script>

<div class="flex flex-col gap-3" data-part="module-log">
  {#if missing}
    <p class="text-sm" data-part="log-gone">{t(`${KEY}.gone`)}</p>
  {:else if refused}
    <p class="text-sm" data-part="log-refused">
      {t('app.settings.extensions.log.refused')}
    </p>
  {:else if log === undefined && fault !== undefined}
    <div data-part="log-failed">
      <FaultAlert fault={describeExtensionFault(fault, t)} />
    </div>
  {:else if log === undefined}
    <p class="text-sm text-muted-foreground" data-part="log-loading">
      {t(`${KEY}.loading`)}
    </p>
  {:else if log.lines.length === 0}
    <p class="text-sm text-muted-foreground" data-part="log-empty">
      {t('app.settings.extensions.log.empty')}
    </p>
  {:else}
    <CodeBlock
      label={t('app.settings.extensions.log.label')}
      value={logText(log)}
      heightClass="h-96"
    />
  {/if}
  <p class="text-xs text-muted-foreground">{t(`${KEY}.cleared`)}</p>
</div>
