<!--
  The buttons of a tool. Assign, or Change when a material is
  assigned, while the printer is connected. Unload on a print head of a
  connected printer that reports a material; the parent says which heads may,
  as the printer unloads the active one. Unassign on a slot, or on any tool of
  a printer that is not connected. Add to Materials for an NFC spool the
  module does not hold yet.

  A button the rules refuse is disabled, and one info mark beside the buttons
  says why, each reason once.
-->
<script lang="ts">
  import { Button } from '@prusa/connect-ui/primitives';

  import type { PrinterTools } from '../api';
  import { heldHost } from '../host';
  import TapTooltip from '../tap-tooltip.svelte';
  import { reasonText } from './reasons';
  import ToolIcon from './tool-icon.svelte';
  import type { ToolView } from './tool-view';

  let {
    view,
    printer,
    unloadable,
    working,
    stacked,
    onassign,
    onunload,
    onunassign,
    onadd,
  }: {
    view: ToolView;
    printer: PrinterTools;
    /** Whether this tool is a print head the printer would unload. */
    unloadable: boolean;
    /** A change is in flight on some tool. */
    working: boolean;
    /** Full-width buttons one under another, as on a tile. */
    stacked: boolean;
    onassign: () => void;
    onunload: () => void;
    onunassign: () => void;
    onadd: () => void;
  } = $props();

  const t = heldHost().t;

  const tool = $derived(view.tool);
  const operate = $derived(printer.may_operate);
  const reported = $derived(tool.reported?.abbreviation ?? '');

  const showUnload = $derived(
    printer.connected && unloadable && view.circle !== '',
  );
  const showUnassign = $derived(
    tool.assignment !== null && (!printer.connected || tool.kind === 'slot'),
  );
  /** An NFC spool the printer read that Materials does not hold yet. */
  const showAdd = $derived(
    tool.from_nfc &&
      (tool.reported?.instance_uuid ?? '') !== '' &&
      tool.assignment?.spool_id === undefined,
  );

  const assignWhy = $derived(
    reasonText(t, tool.assign, 'assign', printer.state, reported),
  );
  const unloadWhy = $derived(
    tool.unassign === 'printing'
      ? t('materials.tools.unloadPrinting')
      : reasonText(t, tool.unassign, 'unassign', printer.state, reported),
  );
  const unassignWhy = $derived(
    reasonText(t, tool.unassign, 'unassign', printer.state, reported),
  );

  const reasons = $derived(
    operate
      ? [
          ...new Set(
            [
              printer.connected ? assignWhy : undefined,
              showUnload ? unloadWhy : undefined,
              showUnassign ? unassignWhy : undefined,
            ].filter((r) => r !== undefined),
          ),
        ]
      : [t('materials.printer.notOperator')],
  );
</script>

<div
  data-part="tool-actions"
  class={stacked
    ? 'flex w-full items-end gap-1'
    : 'flex flex-wrap items-center gap-2'}
>
  <div class={stacked ? 'flex flex-1 flex-col gap-1' : 'contents'}>
    {#if printer.connected}
      <Button
        size="sm"
        variant="secondary"
        disabled={!operate || tool.assign !== '' || working}
        onclick={onassign}
      >
        {view.name === undefined
          ? t('materials.printer.assign')
          : t('materials.printer.change')}
      </Button>
    {/if}
    {#if showUnload}
      <Button
        size="sm"
        variant="secondary"
        disabled={!operate || tool.unassign !== '' || working}
        onclick={onunload}
      >
        {t('materials.tools.unload')}
      </Button>
    {/if}
    {#if showUnassign}
      <Button
        size="sm"
        variant="secondary"
        disabled={!operate || tool.unassign !== '' || working}
        onclick={onunassign}
      >
        {t('materials.printer.unassign')}
      </Button>
    {/if}
    {#if showAdd}
      <Button
        size="sm"
        variant="secondary"
        disabled={!operate || working}
        onclick={onadd}
      >
        {t('materials.printer.addToMaterials')}
      </Button>
    {/if}
  </div>
  {#if reasons.length > 0}
    <TapTooltip text={reasons}>
      <span data-part="reasons" class="inline-flex text-primary">
        <ToolIcon name="info" class="size-4" />
      </span>
    </TapTooltip>
  {/if}
</div>
