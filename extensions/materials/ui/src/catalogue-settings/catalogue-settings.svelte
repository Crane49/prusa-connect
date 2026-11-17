<!--
  The catalogue the module holds, and the button that loads a newer one.

  A settings pane takes no props. The host draws it on the Extensions screen
  with no heading, so the pane names itself, and that screen's ground is the
  pane's own colour, so the pane is edged. Whoever opens the pane while an
  import runs (somebody else may have started it) sees it finish: the poll
  starts on mount and ends with the host's signal or when the pane goes.
-->
<script lang="ts">
  import { formatDateTime } from '@prusa/connect-ui/i18n';
  import {
    Button,
    FaultAlert,
    ListingSkeleton,
    SettingsPane,
  } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { heldCatalogue } from '../catalogue.svelte';
  import FaultNote from '../materials-page/fault-note.svelte';
  import { heldHost } from '../host';
  import { heldList } from '../list.svelte';

  const catalogue = heldCatalogue();
  const list = heldList();
  const host = heldHost();
  const t = host.t;

  const status = $derived(catalogue.status);
  const meta = $derived(status?.meta);
  const running = $derived(status?.import.phase === 'running');
  const counts = $derived(
    meta
      ? ([
          ['brands', meta.brands],
          ['materials', meta.materials],
          ['packages', meta.packages],
          ['containers', meta.containers],
        ] as const)
      : [],
  );

  // The step a failed import stopped at; the error itself is in the module's log.
  const failedText = {
    download: 'materials.settings.failedDownload',
    catalogue: 'materials.settings.failedCatalogue',
    store: 'materials.settings.failedStore',
  } as const;

  // The server keeps the last import's outcome until it restarts, so it is
  // announced only to a pane that saw the import run.
  let seen = $state(false);
  $effect(() => {
    if (running) seen = true;
  });

  // Ends with the host's signal or when this pane is destroyed.
  const ended = new AbortController();
  const signal = AbortSignal.any([ended.signal, host.signal]);

  onMount(() => {
    void catalogue.read().then(() => catalogue.poll(signal));

    return () => ended.abort();
  });

  const download = async (): Promise<void> => {
    if (await catalogue.download()) {
      seen = true;
      catalogue.poll(signal);
    }
  };
</script>

<SettingsPane title={t('materials.settings.title')} edged>
  <div class="flex flex-col gap-3 text-sm">
    {#if !catalogue.loaded}
      <ListingSkeleton label={t('materials.settings.reading')} rows={3} />
    {:else if meta}
      <dl class="grid grid-cols-2 gap-x-4 gap-y-1">
        <dt class="text-muted-foreground">{t('materials.settings.version')}</dt>
        <dd>{meta.last_modified}</dd>
        <dt class="text-muted-foreground">{t('materials.settings.hash')}</dt>
        <dd><code>{meta.data_hash.slice(0, 12)}</code></dd>
        {#each counts as [name, value] (name)}
          <dt class="text-muted-foreground">
            {t(`materials.settings.${name}`)}
          </dt>
          <dd data-part={name}>{value}</dd>
        {/each}
        {#if meta.skipped > 0}
          <dt class="text-muted-foreground">
            {t('materials.settings.skipped')}
          </dt>
          <dd data-part="skipped">{meta.skipped}</dd>
        {/if}
        <dt class="text-muted-foreground">
          {t('materials.settings.imported')}
        </dt>
        <dd>{formatDateTime(meta.imported_at)}</dd>
      </dl>
    {:else}
      <p>{t('materials.settings.none')}</p>
    {/if}

    {#if status?.import.phase === 'failed'}
      <FaultAlert
        fault={{
          code: undefined,
          detail: undefined,
          title: t(failedText[status.import.reason ?? 'download']),
        }}
      />
    {:else if seen && status?.import.phase === 'idle' && status.import.finished}
      <p data-part="result" class="text-muted-foreground">
        {status.import.unchanged
          ? t('materials.settings.unchanged')
          : t('materials.settings.updated')}
      </p>
    {/if}

    {#if running}
      <p data-part="running" class="text-muted-foreground">
        {t('materials.settings.running')}
      </p>
    {/if}

    <FaultNote fault={catalogue.fault} />

    {#if list.mayWrite}
      <div>
        <Button
          size="sm"
          disabled={running || catalogue.busy}
          onclick={download}
        >
          {t('materials.settings.download')}
        </Button>
      </div>
    {/if}
  </div>
</SettingsPane>
