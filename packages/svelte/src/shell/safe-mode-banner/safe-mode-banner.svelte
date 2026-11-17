<!--
  The strip over every page while the server runs with every module off. It
  reads the warning tokens the Alert does, but sits flush with the page edges
  and announces itself as a status rather than an alert: it is a standing
  condition, not an interruption.
-->
<script lang="ts">
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import { describeFault, formatDateTime, t } from '../../i18n';
  import { Banner, Button } from '../../primitives';
  import type { SafeModeBannerProps } from './types';

  let {
    since,
    restarting,
    timedOut,
    allowed,
    refusal,
    error,
    onRestartNormally,
  }: SafeModeBannerProps = $props();

  const fault = $derived(
    error === undefined || refusal !== undefined
      ? undefined
      : describeFault(error),
  );
</script>

<Banner icon={TriangleAlertIcon}>
  <p>
    {t('shell.safeMode.banner', { since: formatDateTime(since) })}
    {#if timedOut}
      <span class="block">{t('shell.safeMode.timeout')}</span>
    {/if}
    {#if refusal === 'forbidden'}
      <span class="block">{t('settingsServer.restart.locked')}</span>
    {:else if refusal === 'upgrade-busy'}
      <span class="block">{t('settingsServer.restart.conflict')}</span>
    {/if}
    {#if fault}<span class="block">{fault.title}</span>{/if}
  </p>
  {#snippet actions()}
    {#if allowed === true && refusal !== 'forbidden'}
      <!-- aria-disabled keeps focus on the button while the restart runs. -->
      <Button
        variant="secondary"
        aria-disabled={restarting}
        aria-busy={restarting}
        onclick={() => {
          if (!restarting) onRestartNormally();
        }}
      >
        {restarting
          ? t('shell.safeMode.restarting')
          : t('shell.safeMode.restartNormally')}
      </Button>
    {/if}
  {/snippet}
</Banner>
