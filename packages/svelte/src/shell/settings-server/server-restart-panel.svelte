<!--
  Restarting the server, by hand. A normal run offers the restart into safe
  mode behind a confirmation, because it stops every connection and module;
  a safe-mode run offers the one way out. The page draws what the data layer
  reports and asks it nothing: `phase` says how far a restart has got.
-->
<script lang="ts">
  import { describeFault, t } from '../../i18n';
  import {
    Alert,
    AlertDescription,
    Button,
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    ConfirmDialog,
    FaultAlert,
  } from '../../primitives';
  import type { ServerRestartPanelProps } from './types';

  let {
    safeMode,
    allowed,
    phase,
    refusal,
    error,
    readError,
    onRetryRead,
    confirming = $bindable(false),
    onRestart,
  }: ServerRestartPanelProps = $props();

  const busy = $derived(phase === 'sending' || phase === 'waiting');
  const fault = $derived(
    phase === 'failed' && error !== undefined && refusal === undefined
      ? describeFault(error)
      : undefined,
  );
  const readFault = $derived(
    allowed === undefined && readError !== undefined
      ? describeFault(readError)
      : undefined,
  );
  const PROGRESS = {
    sending: 'settingsServer.restart.sending',
    waiting: 'settingsServer.restart.waiting',
    timeout: 'settingsServer.restart.timeout',
  } as const;
  const progress = $derived.by(() => {
    if (refusal === 'upgrade-busy') return 'settingsServer.restart.conflict';
    if (phase === 'sending' || phase === 'waiting' || phase === 'timeout')
      return PROGRESS[phase];
    return undefined;
  });
</script>

{#snippet safeTrigger({ props }: { props: Record<string, unknown> })}
  <Button {...busy ? {} : props} variant="secondary" aria-disabled={busy}>
    {t('settingsServer.restart.safe')}
  </Button>
{/snippet}

<Card>
  <CardHeader>
    <CardTitle
      ><h2 class="contents">{t('settingsServer.restart.title')}</h2></CardTitle
    >
    <CardDescription>
      {safeMode
        ? t('settingsServer.restart.safeOn')
        : t('settingsServer.restart.description')}
    </CardDescription>
  </CardHeader>
  <CardContent class="flex flex-col items-start gap-4">
    {#if allowed === false || refusal === 'forbidden'}
      <p class="text-sm text-muted-foreground">
        {t('settingsServer.restart.locked')}
      </p>
    {:else if allowed !== true}
      {#if readFault !== undefined}
        <p class="text-sm text-muted-foreground">
          {t('settingsServer.restart.unread')}
        </p>
      {/if}
    {:else if safeMode}
      <Button
        aria-disabled={busy}
        aria-busy={busy}
        onclick={() => {
          if (!busy) onRestart('normal');
        }}
      >
        {t('settingsServer.restart.normal')}
      </Button>
    {:else}
      <p class="text-sm text-muted-foreground">
        {t('settingsServer.restart.safeHelp')}
      </p>
      <ConfirmDialog
        bind:open={confirming}
        trigger={safeTrigger}
        title={t('settingsServer.restart.confirmTitle')}
        description={t('settingsServer.restart.confirm')}
        confirmLabel={t('settingsServer.restart.safe')}
        onConfirm={() => {
          confirming = false;
          onRestart('safe');
        }}
      />
    {/if}
    {#if progress !== undefined}
      <Alert
        variant={phase === 'timeout' || refusal === 'upgrade-busy'
          ? 'warning'
          : 'info'}
        role="status"
      >
        <AlertDescription>{t(progress)}</AlertDescription>
      </Alert>
    {/if}
    <FaultAlert {fault} />
    <FaultAlert fault={readFault}>
      {#snippet actions()}
        <Button variant="secondary" onclick={onRetryRead}>
          {t('settingsServer.restart.retry')}
        </Button>
      {/snippet}
    </FaultAlert>
  </CardContent>
</Card>
