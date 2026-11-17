<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    PRINTER_STATE_LABEL_KEY,
    PRINTER_STATE_SEVERITY_TIER,
    type PrinterSeverityTier,
    type PrinterState,
    SEVERITY_FILTER_LABEL_KEY,
    SEVERITY_FILTER_TIERS,
  } from '../../domain/printer-state';
  import { t } from '../../i18n';
  import PrinterStateFilter from './printer-state-filter.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/PrinterStateFilter',
    component: PrinterStateFilter,
  });

  interface Printer {
    id: string;
    name: string;
    state: PrinterState;
  }

  /** What a farm is picked from, by hand: one printer per interesting tier. */
  const FLEET: Printer[] = [
    { id: '1', name: 'Farm 01', state: 'PRINTING' },
    { id: '2', name: 'Farm 02', state: 'PRINTING' },
    { id: '3', name: 'Farm 03', state: 'IDLE' },
    { id: '4', name: 'Farm 04', state: 'PAUSED' },
    { id: '5', name: 'Farm 05', state: 'ATTENTION' },
    { id: '6', name: 'Farm 06', state: 'ERROR' },
    { id: '7', name: 'Farm 07', state: 'OFFLINE' },
    { id: '8', name: 'Farm 08', state: 'FINISHED' },
  ];

  const TIERS = [...SEVERITY_FILTER_TIERS];
  const STATES = [...new Set(FLEET.map((printer) => printer.state))];

  const stateLabel = (value: string): string =>
    t(PRINTER_STATE_LABEL_KEY[value as PrinterState]);

  const tierLabel = (value: string): string =>
    t(SEVERITY_FILTER_LABEL_KEY[value as PrinterSeverityTier]);

  const tierOf = (printer: Printer): PrinterSeverityTier =>
    PRINTER_STATE_SEVERITY_TIER[printer.state];

  interface Held {
    tier: string[];
    state: string[];
  }

  const matches = (picked: string[], value: string): boolean =>
    picked.length === 0 || picked.includes(value);

  /**
   * A count answers how many a person would get, so the other dimension is
   * applied and this one's own is ignored.
   */
  const countBy = (
    fleet: Printer[],
    values: string[],
    of: (printer: Printer) => string,
  ): Map<string, number> =>
    new Map(
      values.map((value) => [
        value,
        fleet.filter((printer) => of(printer) === value).length,
      ]),
    );
</script>

<script lang="ts">
  const held = $state({
    both: { tier: [] as string[], state: [] as string[] },
    stateOnly: { tier: [] as string[], state: [] as string[] },
    narrowing: { tier: [] as string[], state: [] as string[] },
  });

  const byTier = (picked: Held): Printer[] =>
    FLEET.filter((printer) => matches(picked.tier, tierOf(printer)));

  const byState = (picked: Held): Printer[] =>
    FLEET.filter((printer) => matches(picked.state, printer.state));

  const visible = (picked: Held): Printer[] =>
    FLEET.filter(
      (printer) =>
        matches(picked.tier, tierOf(printer)) &&
        matches(picked.state, printer.state),
    );
</script>

<!--
  Both dimensions in one popover: Severity above the divider, State below. The
  trigger's summary spans both, because it answers what this list is narrowed
  by and that does not care which group a value came from.
-->
<Story name="Default" asChild>
  <div class="flex w-105 flex-col gap-4">
    <PrinterStateFilter
      severity={{
        values: TIERS,
        labelFor: tierLabel,
        counts: countBy(byState(held.both), TIERS, tierOf),
        selected: held.both.tier,
        onSelectedChange: (next: string[]) => {
          held.both.tier = next;
        },
      }}
      state={{
        values: STATES,
        labelFor: stateLabel,
        counts: countBy(byTier(held.both), STATES, (printer) => printer.state),
        selected: held.both.state,
        onSelectedChange: (next: string[]) => {
          held.both.state = next;
        },
      }}
    />
    <p class="text-xs text-muted-foreground">
      {visible(held.both).length} of {FLEET.length} printers
    </p>
    <ul class="flex flex-col gap-1">
      {#each visible(held.both) as printer (printer.id)}
        <li
          class="flex items-center justify-between rounded-sm border border-border px-2 py-1 text-xs"
        >
          <span class="truncate">{printer.name}</span>
          <span class="text-muted-foreground">{stateLabel(printer.state)}</span>
        </li>
      {/each}
    </ul>
  </div>
</Story>

<!-- Severity omitted, so the facet is the raw state dimension alone. -->
<Story name="State Only" asChild>
  <div class="flex w-105 flex-col gap-4">
    <PrinterStateFilter
      state={{
        values: STATES,
        labelFor: stateLabel,
        counts: countBy(
          byTier(held.stateOnly),
          STATES,
          (printer) => printer.state,
        ),
        selected: held.stateOnly.state,
        onSelectedChange: (next: string[]) => {
          held.stateOnly.state = next;
        },
      }}
    />
    <p class="text-xs text-muted-foreground">
      {visible(held.stateOnly).length} of {FLEET.length} printers
    </p>
    <ul class="flex flex-col gap-1">
      {#each visible(held.stateOnly) as printer (printer.id)}
        <li
          class="flex items-center justify-between rounded-sm border border-border px-2 py-1 text-xs"
        >
          <span class="truncate">{printer.name}</span>
          <span class="text-muted-foreground">{stateLabel(printer.state)}</span>
        </li>
      {/each}
    </ul>
  </div>
</Story>

<!--
  The two things this component exists to do: say what is picked without being
  reopened, and actually narrow the list.
-->
<Story name="Picking Narrows The Fleet" asChild>
  <div class="flex w-105 flex-col gap-4">
    <PrinterStateFilter
      severity={{
        values: TIERS,
        labelFor: tierLabel,
        counts: countBy(byState(held.narrowing), TIERS, tierOf),
        selected: held.narrowing.tier,
        onSelectedChange: (next: string[]) => {
          held.narrowing.tier = next;
        },
      }}
      state={{
        values: STATES,
        labelFor: stateLabel,
        counts: countBy(
          byTier(held.narrowing),
          STATES,
          (printer) => printer.state,
        ),
        selected: held.narrowing.state,
        onSelectedChange: (next: string[]) => {
          held.narrowing.state = next;
        },
      }}
    />
    <p class="text-xs text-muted-foreground">
      {visible(held.narrowing).length} of {FLEET.length} printers
    </p>
    <ul class="flex flex-col gap-1">
      {#each visible(held.narrowing) as printer (printer.id)}
        <li
          class="flex items-center justify-between rounded-sm border border-border px-2 py-1 text-xs"
        >
          <span class="truncate">{printer.name}</span>
          <span class="text-muted-foreground">{stateLabel(printer.state)}</span>
        </li>
      {/each}
    </ul>
  </div>
</Story>
