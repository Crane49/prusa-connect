<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import RequestList from './request-list.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Request List',
    component: RequestList,
  });
</script>

<Story name="Every Kind Of Request" asChild>
  <RequestList
    instance="buddy-debug-logs"
    label="This extension asks for:"
    requests={[
      { kind: 'permission', permission: 'printers.read' },
      { kind: 'event', endpoint: 'spool.low', name: 'low' },
      { kind: 'absentPeople' },
      { kind: 'records', mib: 64 },
      { kind: 'memory', mib: 192 },
      { kind: 'data' },
      { kind: 'database' },
      {
        kind: 'listen',
        name: 'web',
        network: 'tcp',
        port: 8080,
        why: 'serves its own page',
      },
      { kind: 'other', line: 'a request this page cannot read' },
    ]}
  />
</Story>

<Story name="One Permission" asChild>
  <RequestList
    instance="buddy-debug-logs"
    label="This extension asks for:"
    requests={[{ kind: 'permission', permission: 'printers.read' }]}
  />
</Story>
