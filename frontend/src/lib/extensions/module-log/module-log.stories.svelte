<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { ApiError } from '@prusa/connect';

  import ModuleLog from './module-log.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Module Log',
    component: ModuleLog,
  });

  const at = new Date().toISOString();
</script>

<Story name="Lines" asChild>
  <ModuleLog
    log={{
      lines: [
        { at, text: 'listening on :8080' },
        { at, text: 'spool 12 is low' },
      ],
    }}
  />
</Story>

<Story name="Loading" asChild>
  <ModuleLog />
</Story>

<Story name="Failed" asChild>
  <ModuleLog
    fault={new ApiError(500, {
      type: 'about:blank',
      status: 500,
      retryable: true,
      code: 'core.internal/FAILED',
      title: 'Failed',
      detail: 'The server could not read it.',
      params: {},
      trace_id: 'trace-1',
    })}
  />
</Story>

<Story name="Refused" asChild>
  <ModuleLog refused />
</Story>

<Story name="Empty" asChild>
  <ModuleLog log={{ lines: [] }} />
</Story>

<Story name="Gone" asChild>
  <ModuleLog missing />
</Story>
