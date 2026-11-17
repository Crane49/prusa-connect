<!--
  After Confirm: the server writes the answers and restarts, and the page
  waits for it. It ends on the ready screen, or says why it cannot.
-->
<script lang="ts">
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import type { SetupPhase } from '../../data';
  import { t } from '../../i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    Spinner,
    Progress,
  } from '../../primitives';

  interface StepWaitProps {
    phase: SetupPhase;
    /** How far the wait has gone, from 0 to 1. */
    elapsed: number;
    /** The address the server moved to, for the `moved` phase. */
    moves: string | undefined;
    busy: boolean;
    /** Why the server went back to its earlier configuration, when it did. */
    failure: string | undefined;
    /** Whether the server runs without its web app after the restart. */
    webOff: boolean;
    onretry: () => void;
    onedit: () => void;
    onfinish: () => void;
  }

  let {
    phase,
    elapsed,
    moves,
    busy,
    failure,
    webOff,
    onretry,
    onedit,
    onfinish,
  }: StepWaitProps = $props();
</script>

<div
  class="flex flex-col items-start gap-6"
  data-part="wait"
  data-phase={phase}
  aria-live="polite"
>
  {#if phase === 'sending' || phase === 'waiting'}
    <div class="flex items-center gap-3">
      <Spinner size="lg" tone="primary" />
      <span class="text-lg font-medium">
        {phase === 'sending'
          ? t('setupWizard.wait.sending')
          : t('setupWizard.wait.lead')}
      </span>
    </div>
    <Progress
      class="w-full"
      value={Math.round(elapsed * 100)}
      aria-label={t('setupWizard.wait.title')}
    />
  {:else if phase === 'moved' && moves !== undefined}
    <p>{t('setupWizard.wait.moved', { address: moves })}</p>
    <Button variant="default">
      {#snippet child({ props }: { props: Record<string, unknown> })}
        <a {...props} href={moves}>
          {t('setupWizard.wait.open', { address: moves })}
        </a>
      {/snippet}
    </Button>
  {:else if phase === 'timeout'}
    <div class="flex items-center gap-3 text-warning-text">
      <TriangleAlertIcon class="size-6 shrink-0" aria-hidden="true" />
      <p>{t('setupWizard.timeout.lead')}</p>
    </div>
    <div class="flex flex-wrap gap-3">
      <Button onclick={onretry}>{t('setupWizard.timeout.retry')}</Button>
      <Button variant="ghost" onclick={onedit}
        >{t('setupWizard.timeout.edit')}</Button
      >
    </div>
  {:else if phase === 'ready'}
    <div class="flex items-center gap-3">
      <CircleCheckIcon
        class="size-8 shrink-0 text-success-text"
        aria-hidden="true"
      />
      <p class="text-muted-foreground">
        {webOff ? t('setupWizard.done.webOff') : t('setupWizard.done.lead')}
      </p>
    </div>
    {#if failure !== undefined}
      <Alert variant="warning" data-part="kept-earlier">
        <AlertTitle>{t('setupWizard.done.keptEarlier')}</AlertTitle>
        <AlertDescription>
          <p class="font-mono text-xs">{failure}</p>
        </AlertDescription>
      </Alert>
    {/if}
    {#if !webOff}
      <Button size="lg" disabled={busy} onclick={onfinish}
        >{t('setupWizard.done.open')}</Button
      >
    {/if}
  {/if}
</div>
