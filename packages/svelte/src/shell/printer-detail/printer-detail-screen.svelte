<!-- One printer, and everything this product knows about it. -->
<script lang="ts">
  import HammerIcon from '@lucide/svelte/icons/hammer';

  import { faultOf } from '../../data';
  import {
    PRINTER_STATE_LABEL_KEY,
    printerStateOf,
    STATE_PATH,
    textAt,
  } from '../../domain';
  import { describeFault, t } from '../../i18n';
  import {
    Badge,
    Card,
    CardContent,
    DetailHeader,
    DetailTabs,
    EmptyState,
    FaultAlert,
    NoLongerHere,
    PageTrail,
    PrinterIcon,
    Spinner,
    type DetailTab,
  } from '../../primitives';
  import {
    printerArtworkOf,
    retiredReasonOf,
    stateBadgeVariant,
  } from '../printers/presentation';
  import { mayAdminister } from './info/permission';
  import PrinterRename from './info/printer-rename.svelte';
  import PrinterSwitcher from './printer-switcher.svelte';
  import TelemetryChips from './telemetry-chips.svelte';
  import { PRINTER_DETAIL_TABS } from './types';
  import type { PrinterDetailScreenProps, PrinterDetailTabId } from './types';

  let {
    deviceId,
    device,
    telemetry,
    commands,
    peer,
    printerCount,
    onSelectPeer,
    tab,
    onTabChange,
    panels,
    moduleTabs,
    actions,
    prompt,
    fault,
    loading = false,
    administration,
    printersHref,
  }: PrinterDetailScreenProps = $props();

  // The path always names the printer.
  const name = $derived(device?.name ?? deviceId);
  const waiting = $derived(loading || device === undefined);

  // The fleet answered and holds no such printer, or refused it. Any other
  // refusal is a fault to show, not a printer that is gone.
  const missing = $derived(
    !loading &&
      device === undefined &&
      (fault === undefined || [403, 404].includes(faultOf(fault).status ?? 0)),
  );

  // A disconnected machine reports nothing.
  const state = $derived(
    device?.connected === true
      ? printerStateOf(textAt(device, STATE_PATH) ?? 'UNKNOWN')
      : 'OFFLINE',
  );
  const refusal = $derived(
    fault === undefined ? undefined : describeFault(fault),
  );

  const label: Record<PrinterDetailTabId, string> = $derived({
    status: t('printerDetail.tab.status'),
    controls: t('printerDetail.tab.controls'),
    files: t('printerDetail.tab.files'),
    cameras: t('printerDetail.tab.cameras'),
    info: t('printerDetail.tab.info'),
    history: t('printerDetail.tab.history'),
    commands: t('printerDetail.tab.commands'),
  });

  // A retired printer's page is its record: no tab sends a command to it.
  const retired = $derived(device?.retired);
  const RETIRED_TABS: readonly PrinterDetailTabId[] = ['info', 'history'];

  const tabs: DetailTab[] = $derived([
    ...PRINTER_DETAIL_TABS.filter(
      (id) =>
        (retired === undefined || RETIRED_TABS.includes(id)) &&
        // The Cameras tab is drawn only where the page hands it a panel: a
        // tab that would only say this build draws nothing is left out.
        (id !== 'cameras' || panels?.cameras !== undefined),
    ).map((id) => ({
      id,
      label: label[id],
      panel: panels?.[id],
    })),
    ...(retired === undefined ? (moduleTabs ?? []) : []),
  ]);
  const active = $derived(
    retired !== undefined && !RETIRED_TABS.includes(tab as PrinterDetailTabId)
      ? 'history'
      : tab,
  );

  const admin = $derived(
    device !== undefined && retired === undefined && mayAdminister(device)
      ? administration
      : undefined,
  );

  // The pair is the only shape with a switcher.
  const pair = $derived.by(() => {
    if (device === undefined || peer === undefined || printerCount !== 2) {
      return undefined;
    }

    return peer.side === 'before'
      ? [peer.device, device]
      : [device, peer.device];
  });
</script>

<div class="mx-auto flex w-full max-w-7xl flex-col gap-6">
  <PageTrail
    trail={[{ label: t('printerDetail.printers'), href: printersHref }]}
    page={name}
  />

  {#if pair && device}
    <PrinterSwitcher
      printers={pair}
      chosenId={device.id}
      onChoose={() => onSelectPeer?.()}
    />
  {/if}

  {#if !missing}
    <FaultAlert fault={refusal} />
  {/if}

  {#if missing}
    <NoLongerHere
      headingLevel="h1"
      heading={t('printerDetail.missing.heading')}
      description={t('printerDetail.missing.detail')}
      linkLabel={t('printerDetail.printers')}
      href={printersHref}
    />
  {:else}
    <DetailHeader
      title={name}
      loading={waiting}
      {media}
      {titleAside}
      {meta}
      actions={retired ? undefined : actions}
    />

    {#if retired}
      <p class="text-sm text-muted-foreground">
        {retiredReasonOf(retired.reason)}
      </p>
    {/if}

    {#if prompt}{@render prompt()}{/if}

    <DetailTabs
      {tabs}
      {active}
      onChange={(id: string) => onTabChange(id)}
      fallback={notBuilt}
    />
  {/if}
</div>

{#snippet media()}
  {#if device}
    <PrinterIcon
      model={printerArtworkOf(device)}
      reported={device.model}
      class="size-32"
    />
  {/if}
{/snippet}

{#snippet titleAside()}
  {#if device}
    <Badge
      mono
      size="md"
      variant={retired ? 'secondary' : stateBadgeVariant(state)}
    >
      {retired ? t('printers.retired') : t(PRINTER_STATE_LABEL_KEY[state])}
    </Badge>
    {#if commands.readingDrive && !retired}
      <Badge variant="secondary" size="md">
        <Spinner />
        {t('printerDetail.readingDrive')}
      </Badge>
    {/if}
    {#if admin}
      <PrinterRename name={device.name} administration={admin} trigger="icon" />
    {/if}
  {/if}
{/snippet}

{#snippet meta()}
  {#if device && !retired}
    <TelemetryChips {telemetry} {device} {commands} />
  {/if}
{/snippet}

{#snippet notBuilt()}
  <Card>
    <CardContent>
      <EmptyState
        icon={HammerIcon}
        heading={t('printerDetail.panel.notBuilt')}
        headingLevel="h2"
        description={t('printerDetail.panel.notBuiltDetail')}
      />
    </CardContent>
  </Card>
{/snippet}
