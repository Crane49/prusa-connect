<!--
  The steps on the printer's own display, for a person who may not send it
  G-code, or who prefers to set it by hand. Folded: the button is the short
  way, and these steps are what it does.
-->
<script lang="ts">
  import {
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
  } from '@prusa/connect-ui/primitives';

  import { heldSession } from '../session';

  let {
    host: serverHost,
    syslogPort,
    metricsPort,
    open = $bindable(false),
    onOpenChange,
  }: {
    host: string;
    syslogPort: number;
    metricsPort: number;
    open?: boolean;
    /** Told when the person folds or unfolds the steps. */
    onOpenChange?: ((open: boolean) => void) | undefined;
  } = $props();

  const t = heldSession().host.t;
</script>

<Collapsible
  bind:open
  onOpenChange={(next: boolean) => {
    onOpenChange?.(next);
  }}
  data-part="guide"
>
  <CollapsibleTrigger variant="row">
    <span class="text-sm font-medium">{t('buddyDebugLogs.guide.title')}</span>
  </CollapsibleTrigger>
  <CollapsibleContent>
    <ol class="grid list-decimal gap-1 ps-8 pt-3 text-sm">
      <li>{t('buddyDebugLogs.guide.step1')}</li>
      <li>{t('buddyDebugLogs.guide.step2', { host: serverHost })}</li>
      <li>
        {t('buddyDebugLogs.guide.step3', {
          syslog: String(syslogPort),
          metrics: String(metricsPort),
        })}
      </li>
      <li>{t('buddyDebugLogs.guide.step4')}</li>
      <li>{t('buddyDebugLogs.guide.step5')}</li>
    </ol>
    <p class="ps-3 pt-2 text-xs text-muted-foreground">
      {t('buddyDebugLogs.guide.note')}
    </p>
  </CollapsibleContent>
</Collapsible>
