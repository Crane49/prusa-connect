<!--
  A module the server switched off after it kept failing, one alert each. It is
  drawn from the module's status, so it is still there after a reload.
-->
<script lang="ts">
  import { formatRelative, translateFor } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
  } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import type { ExtensionRow } from '../rows';

  let {
    rows,
    href,
  }: {
    rows: readonly ExtensionRow[];
    /** The address of the module's own page. */
    href: (row: ExtensionRow) => string;
  } = $props();

  const t = translateFor(APP_NAMESPACE);
  const KEY = 'app.settings.extensions.switchedOff';

  // The listing row already says why the module stopped, so the alert says when.
  const since = (iso: string): string => {
    const minutes = Math.round((Date.parse(iso) - Date.now()) / 60_000);
    if (Math.abs(minutes) < 60) return formatRelative(minutes, 'minute');
    if (Math.abs(minutes) < 1440)
      return formatRelative(Math.round(minutes / 60), 'hour');

    return formatRelative(Math.round(minutes / 1440), 'day');
  };

  const off = $derived(
    rows.flatMap((row) => {
      const memory = row.instance?.status.memory;

      return row.instance === undefined || memory?.switched_off === undefined
        ? []
        : [
            {
              row,
              held: memory.held_until_restart,
              at: memory.switched_off.at,
            },
          ];
    }),
  );
</script>

{#each off as { row, held, at } (row.key)}
  <Alert variant="warning" data-part="switched-off" data-extension={row.module}>
    <AlertTitle>{row.title}</AlertTitle>
    <AlertDescription>
      <span class="block">
        {t(`${KEY}.at`, { when: since(at) })}
      </span>
      {#if held}
        <span class="block" data-part="held">{t(`${KEY}.held`)}</span>
      {/if}
      <a
        href={href(row)}
        class="rounded-sm underline underline-offset-4 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
      >
        {t(`${KEY}.open`)}
      </a>
    </AlertDescription>
  </Alert>
{/each}
