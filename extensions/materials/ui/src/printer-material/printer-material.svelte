<!--
  The Tools tab of the printer page: a printer with one tool gets the Extruder
  view with its settings and the material's details; one with several gets a
  tile per tool, four to a row; an MMU gets a group of slot tiles under one
  header. Each tool shows the material on it and what is left of its spool,
  with Assign, Change, Unload and Unassign.

  A control the rules refuse is disabled, and an info mark says why. A person
  who may not operate the printer sees the controls disabled for that reason
  first. A printer the person may not observe is not there for them, so the
  tab draws nothing.
-->
<script lang="ts">
  import { sendCommand } from '@prusa/connect-ui/data';
  import { STATE_PATH, textAt } from '@prusa/connect-ui/domain';
  import { Button, ListingSkeleton } from '@prusa/connect-ui/primitives';
  import { onMount, untrack } from 'svelte';

  import {
    type Detail,
    materialsOn,
    type PrinterTools,
    type Spool,
    type Tool,
  } from '../api';
  import { heldHost } from '../host';
  import { follow, type SpoolChange, type ToolChange } from '../live';
  import FaultNote from '../materials-page/fault-note.svelte';
  import TapTooltip from '../tap-tooltip.svelte';
  import AssignDialog from './assign-dialog.svelte';
  import MmuGroup from './mmu-group.svelte';
  import { refusalText } from './reasons';
  import SingleTool from './single-tool.svelte';
  import ToolActions from './tool-actions.svelte';
  import ToolIcon from './tool-icon.svelte';
  import ToolTile from './tool-tile.svelte';
  import { type ToolView, toolView } from './tool-view';

  let { deviceId }: { deviceId: string } = $props();

  const host = heldHost();
  const t = host.t;
  const service = materialsOn(host);

  let printer = $state.raw<PrinterTools | undefined>(undefined);
  /** The module does not follow this printer, or the person may not see it. */
  let gone = $state(false);
  let fault = $state.raw<unknown>(undefined);
  /** The spools on the tools, by id, for what is left of each. */
  let spools = $state.raw<ReadonlyMap<number, Spool>>(new Map());
  /** The tool a change is in flight on. */
  let working = $state<string | undefined>(undefined);
  /** A change the module refused, and on which tool. */
  let refused = $state.raw<
    | { tool: string; thrown: unknown; action: 'unassign' | 'add' | 'unload' }
    | undefined
  >(undefined);
  /** The tool the assign dialog is open on. */
  let assigning = $state.raw<Tool | undefined>(undefined);

  /** The spools on the tools, read from their materials' spool lists. */
  const readSpools = async (
    tools: readonly Tool[],
  ): Promise<Map<number, Spool>> => {
    const on = tools.flatMap(({ assignment: a }) =>
      a?.spool_id === undefined
        ? []
        : [{ uuid: a.material_uuid, id: a.spool_id }],
    );
    const read = await Promise.all(
      [...new Set(on.map((o) => o.uuid))].map(async (uuid) => {
        const ids = on.filter((o) => o.uuid === uuid).map((o) => o.id);
        const all: Spool[] = [];
        let offset: number | undefined;
        do {
          const page = await service.spools(uuid, offset);
          all.push(...page.spools);
          offset = page.next;
        } while (
          offset !== undefined &&
          !ids.every((id) => all.some((s) => s.id === id))
        );
        return all.filter((s) => ids.includes(s.id));
      }),
    );
    return new Map(read.flat().map((s) => [s.id, s]));
  };

  /** Bumped by each read, so an older answer that lands late is dropped. */
  let reads = 0;
  /** Ends the event stream, and the reads, once the printer is gone. */
  const following = new AbortController();

  const read = async (): Promise<void> => {
    reads += 1;
    const mine = reads;
    try {
      const answer = await service.tools(deviceId);
      if (mine !== reads) return;
      printer = answer;
      fault = undefined;
      // The weight is a detail: a failed read leaves it away and keeps the rows.
      const weighed = await readSpools(answer.tools).catch(() => undefined);
      if (mine === reads && weighed !== undefined) spools = weighed;
    } catch (thrown) {
      if (mine !== reads) return;
      if ((thrown as { code?: unknown }).code === 'materials/NOT_FOUND') {
        gone = true;
        following.abort();
      } else {
        fault = thrown;
      }
    }
  };

  /** How long a burst of telemetry settles before the tools are read again. */
  const SETTLE_MS = 300;
  /**
   * How often the tools are read while the page is in view: the event stream
   * does not tell of a nozzle the printer reports changed.
   */
  const POLL_MS = 15_000;
  let settle: ReturnType<typeof setTimeout> | undefined;

  /** Reads the tools again once a burst of changes settles. */
  const soon = (): void => {
    if (host.signal.aborted || gone) return;
    clearTimeout(settle);
    settle = setTimeout(() => void read(), SETTLE_MS);
  };

  /**
   * Whether a change this module told of is one this printer shows: a change
   * on one of its tools, a change of a spool on one, or a lost place in the
   * stream.
   */
  const ours = (change: ToolChange | SpoolChange | undefined): boolean =>
    change === undefined ||
    ('device_id' in change
      ? change.device_id === deviceId
      : spools.has(change.spool_id));

  onMount(() => {
    // An NFC load, an MMU swap and a spool losing filament as it prints are
    // told of on the event stream, and read at once. A printer the person may
    // not see is not followed.
    void read().then(() => {
      if (gone || following.signal.aborted) return;
      follow(host.client, following.signal, ['tool', 'spool'], (_, change) => {
        if (ours(change)) soon();
      });
    });
    const poll = setInterval(() => {
      if (!document.hidden && !gone) void read();
    }, POLL_MS);
    const stop = (): void => {
      clearInterval(poll);
      clearTimeout(settle);
      following.abort();
    };
    host.signal.addEventListener('abort', stop, { once: true });

    return () => {
      stop();
      host.signal.removeEventListener('abort', stop);
    };
  });

  /** The printer's state word and link, as the host's fleet keeps them live. */
  const live = $derived.by(() => {
    const device = host.fleet.device(deviceId);
    if (device === undefined) return undefined;
    return `${String(device.connected)} ${textAt(device, STATE_PATH) ?? ''}`;
  });
  /** The value the mount read already answers for. */
  let seen: string | undefined;

  // A print that starts or a filament change changes what the rules allow, so
  // the tools are read again once the state settles.
  $effect(() => {
    const now = live;
    untrack(() => {
      if (now === undefined || now === seen) return;
      const first = seen === undefined;
      seen = now;
      if (!first) soon();
    });
  });

  /** Runs one change on a tool, then reads the tools again. */
  const change = async (
    tool: Tool,
    action: 'unassign' | 'add' | 'unload',
  ): Promise<void> => {
    if (working !== undefined) return;
    working = tool.id;
    refused = undefined;
    try {
      if (action === 'add') await service.addReported(deviceId, tool.id);
      else if (action === 'unassign') await service.unassign(deviceId, tool.id);
      else await unload();
      await read();
    } catch (thrown) {
      refused = { tool: tool.id, thrown, action };
    } finally {
      working = undefined;
    }
  };

  /**
   * Asks the printer to unload the filament, as the Controls tab does. The
   * printer unloads its active tool, so only that one offers it.
   */
  const unload = async (): Promise<void> => {
    const result = await sendCommand(
      host.client,
      deviceId,
      'core.command.unload_filament',
    );
    if (!result.ok) {
      // The page draws a fault by its code and its English message.
      throw Object.assign(
        new Error(result.machine_reason ?? t('materials.tools.unloadRefused')),
        { code: result.fault_code ?? undefined },
      );
    }
  };

  const printing = $derived(printer?.state === 'PRINTING');
  const slots = $derived(printer?.tools.filter((x) => x.kind === 'slot') ?? []);
  const heads = $derived(printer?.tools.filter((x) => x.kind === 'head') ?? []);
  /** One print head and no MMU: the page describes that tool in full. */
  const single = $derived(heads.length === 1 && slots.length === 0);
  const viewOf = (tool: Tool): ToolView => {
    const id = tool.assignment?.spool_id;
    const spool = id === undefined ? undefined : spools.get(id);
    return toolView(t, tool, spool, single, printing);
  };
  const headViews = $derived(heads.map(viewOf));

  /** Why the MMU's Unload filament is refused, undefined when it is not. */
  const mmuWhy = (
    slot: Tool,
    loaded: Tool | undefined,
    operate: boolean,
  ): string | undefined => {
    if (!operate) return t('materials.printer.notOperator');
    if (slot.unassign === 'printing')
      return t('materials.tools.unloadPrinting');
    if (slot.unassign !== '') return t('materials.printer.busy');
    if (loaded === undefined) return t('materials.tools.nothingLoaded');
    return undefined;
  };

  /** The MMUs, each the slots of one head, with what its header needs. */
  const mmus = $derived(
    [...new Set(slots.map((s) => s.id.split('.')[0] ?? ''))].flatMap((head) => {
      const group = slots.filter((s) => s.id.startsWith(`${head}.`));
      const [first] = group;
      if (first === undefined) return [];
      const loaded = group.find((s) => s.active);
      return [
        {
          head,
          first,
          views: group.map(viewOf),
          // The printer unloads through the head whatever slot fed it.
          unloads: loaded ?? first,
          why: mmuWhy(first, loaded, printer?.may_operate ?? false),
        },
      ];
    }),
  );

  /** The material the one tool holds, for its details. */
  let detail = $state.raw<Detail | undefined>(undefined);
  const detailed = $derived(
    single ? (heads[0]?.assignment?.material_uuid ?? '') : '',
  );
  $effect(() => {
    const uuid = detailed;
    untrack(() => {
      if (detail?.uuid === uuid) return;
      // Another material's details are wrong while this one's are read.
      detail = undefined;
      if (uuid === '') return;
      service.material(uuid, host.signal).then(
        (read) => {
          if (detailed === uuid) detail = read;
        },
        // The details are a detail: the tab stands without them.
        () => undefined,
      );
    });
  });
</script>

{#snippet toolActions(
  on: PrinterTools,
  view: ToolView,
  unloadable: boolean,
  stacked: boolean,
)}
  {@const failed = refused?.tool === view.tool.id ? refused : undefined}
  <div class="flex w-full flex-col gap-2">
    <ToolActions
      {view}
      printer={on}
      {unloadable}
      working={working !== undefined}
      {stacked}
      onassign={() => {
        refused = undefined;
        assigning = view.tool;
      }}
      onunload={() => void change(view.tool, 'unload')}
      onunassign={() => void change(view.tool, 'unassign')}
      onadd={() => void change(view.tool, 'add')}
    />
    {#if failed}
      {@const text =
        failed.action === 'unassign'
          ? refusalText(
              t,
              failed.thrown,
              'unassign',
              view.tool.reported?.abbreviation ?? '',
            )
          : undefined}
      {#if text}
        <p data-part="refusal" class="text-sm text-destructive">{text}</p>
      {:else}
        <FaultNote fault={failed.thrown} />
      {/if}
    {/if}
  </div>
{/snippet}

{#snippet tile(on: PrinterTools, view: ToolView, unloadable: boolean)}
  <ToolTile {view} open={assigning?.id === view.tool.id}>
    {#snippet actions()}
      {@render toolActions(on, view, unloadable, true)}
    {/snippet}
  </ToolTile>
{/snippet}

{#if !gone}
  <!-- The tab already says Tools, so the section repeats it only to a screen reader. -->
  <section
    data-part="printer-tools"
    aria-label={t('materials.tools.title')}
    class="@container flex flex-col gap-4"
  >
    <FaultNote {fault} />
    {#if printer === undefined}
      {#if fault === undefined}
        <ListingSkeleton label={t('materials.printer.reading')} rows={2} />
      {/if}
    {:else if printer.tools.length === 0}
      <p class="text-sm text-muted-foreground">
        {t('materials.tools.noTools')}
      </p>
    {:else if single && headViews[0] !== undefined}
      {@const view = headViews[0]}
      {@const on = printer}
      <SingleTool {view} {detail}>
        {#snippet actions()}
          {@render toolActions(on, view, true, false)}
        {/snippet}
      </SingleTool>
    {:else}
      {@const on = printer}
      <ul class="tiles grid gap-2">
        {#each headViews as view (view.tool.id)}
          <!-- The printer unloads its active tool, so only that one offers it. -->
          {@render tile(on, view, view.tool.active)}
        {/each}
        {#each mmus as mmu (mmu.head)}
          <MmuGroup
            feeder={mmu.first.feeder ?? ''}
            nozzle={mmu.first.nozzle}
            slots={mmu.views.length}
            unload={on.connected ? mmuUnload : undefined}
          >
            {#each mmu.views as view (view.tool.id)}
              {@render tile(on, view, false)}
            {/each}
          </MmuGroup>
          {#snippet mmuUnload()}
            <span class="flex items-center gap-2">
              {#if mmu.why !== undefined}
                <TapTooltip text={mmu.why}>
                  <span data-part="reasons" class="inline-flex text-primary">
                    <ToolIcon name="info" class="size-4" />
                  </span>
                </TapTooltip>
              {/if}
              <Button
                size="sm"
                variant="secondary"
                disabled={mmu.why !== undefined || working !== undefined}
                onclick={() => void change(mmu.unloads, 'unload')}
              >
                {t('materials.tools.unloadFilament')}
              </Button>
            </span>
          {/snippet}
        {/each}
      </ul>
    {/if}
  </section>
{/if}

<AssignDialog
  {service}
  {deviceId}
  tool={assigning}
  label={assigning === undefined ? '' : viewOf(assigning).label}
  onclose={() => (assigning = undefined)}
  onassigned={() => void read()}
/>

<style>
  /* Four tiles to a row; a narrow screen fits
     as many as it can. */
  .tiles {
    grid-template-columns: repeat(
      4,
      minmax(calc(var(--spacing) * 32), calc(var(--spacing) * 64))
    );
  }

  @container (max-width: 40rem) {
    .tiles {
      grid-template-columns: repeat(
        auto-fit,
        minmax(calc(var(--spacing) * 32), 1fr)
      );
    }
  }
</style>
