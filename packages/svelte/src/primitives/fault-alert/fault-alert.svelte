<script lang="ts">
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import { t } from '../../i18n';
  import { Alert, AlertDescription, AlertTitle } from '../alert';
  import CopyButton from '../copy-button/copy-button.svelte';
  import { reportHref } from './report';
  import type { FaultAlertProps } from './types';

  let { fault, actions, class: className }: FaultAlertProps = $props();

  const report = reportHref();
</script>

{#if fault}
  <Alert variant="destructive" class={className}>
    <TriangleAlertIcon aria-hidden="true" />
    <AlertTitle>{fault.title}</AlertTitle>
    {#if fault.detail !== undefined || fault.action !== undefined || fault.code !== undefined || fault.traceId !== undefined || report !== undefined || actions !== undefined}
      <AlertDescription>
        {#if fault.detail}<span class="block">{fault.detail}</span>{/if}
        {#if fault.action}<span class="block">{fault.action}</span>{/if}
        {#if fault.code}<code class="block text-xs">{fault.code}</code>{/if}
        {#if fault.traceId}
          <span
            class="mt-1 flex items-center gap-1 text-xs text-muted-foreground"
          >
            {t('primitives.faultAlert.traceId')}
            <code>{fault.traceId}</code>
            <CopyButton
              value={fault.traceId}
              label={t('primitives.copyNamed', {
                name: t('primitives.faultAlert.traceId'),
              })}
              size="icon-sm"
            />
          </span>
        {/if}
        {#if report}
          <a
            href={report}
            class="block rounded-sm text-xs underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
          >
            {t('primitives.faultAlert.report')}
          </a>
        {/if}
        {#if actions}
          <span class="mt-1 flex flex-wrap gap-2">{@render actions()}</span>
        {/if}
      </AlertDescription>
    {/if}
  </Alert>
{/if}
