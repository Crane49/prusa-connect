<!--
  What a module asks for, one line each, in words a person reads: each
  permission, each event it publishes, whether it may ask about people it is
  not serving, the room it wants for its records, the memory it recommends
  for its process, a directory of its own, a
  database of its own and each network port it listens on. An empty list says so,
  because "asks for nothing" is something the person is agreeing to as well.
-->
<script lang="ts">
  import { formatNumber, translateFor } from '@prusa/connect-ui/i18n';

  import { APP_NAMESPACE } from '../../i18n';
  import type { ModuleRequest } from '../requests';

  let {
    requests,
    instance,
    label,
    part = 'requests',
  }: {
    requests: readonly ModuleRequest[];
    /** The module's manifest name, which names its folder under `module-data` (not its namespace). */
    instance: string;
    /** The sentence the list follows. */
    label: string;
    /** What a test finds the list by. */
    part?: string;
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  const lineOf = (request: ModuleRequest): string => {
    switch (request.kind) {
      case 'permission':
        return t('app.settings.extensions.request.permission', {
          permission: request.permission,
        });
      case 'check':
        return t('app.settings.extensions.request.check', {
          permission: request.permission,
        });
      case 'event':
        return t('app.settings.extensions.request.event', {
          name: request.name,
        });
      case 'absentPeople':
        return t('app.settings.extensions.request.absentPeople');
      case 'listPeople':
        return t('app.settings.extensions.request.listPeople');
      case 'call':
        return t('app.settings.extensions.request.call', {
          namespace: request.namespace,
        });
      case 'records':
        return t('app.settings.extensions.request.records', {
          size: formatNumber(request.mib),
        });
      case 'memory':
        return t('app.settings.extensions.request.memory', {
          size: formatNumber(request.mib),
        });
      case 'data':
        return t('app.settings.extensions.request.data', { instance });
      case 'database':
        return t('app.settings.extensions.request.database');
      case 'listen':
        return t('app.settings.extensions.request.listen', {
          network: request.network.toUpperCase(),
          port: String(request.port),
          why: request.why,
        });
      case 'other':
        return request.line;
    }
  };

  const lines = $derived(requests.map(lineOf));
</script>

<div class="flex flex-col gap-1 text-sm" data-part={part}>
  {#if lines.length === 0}
    <p>{t('app.settings.extensions.request.none')}</p>
  {:else}
    <p>{label}</p>
    <ul class="list-disc ps-5">
      {#each lines as line, index (index)}
        <li>{line}</li>
      {/each}
    </ul>
  {/if}
</div>
