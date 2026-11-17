<!--
  Filter a fleet by what its printers are doing.

  One facet holding two dimensions, because they are the same question at two
  resolutions. Severity is the coarse one a farm asks first, who needs me.
  State is the precise one it asks second, which of those is paused. Both are
  derived from a printer's state, so splitting them across two triggers made a
  reader open both to see one answer. They are groups inside one popover
  instead, divided and headed.

  The trigger names the dimension and the choice, `State: Critical +2`, never
  an anonymous glyph with a number beside it. Nobody should have to open a
  popover to remember what a list is filtered by.

  Three rules it inherits from the fleet filter bar, all worth keeping:

  1. A value nothing would return stays visible and dimmed rather than
     disappearing, because a list that reshuffles as you tick things is
     impossible to aim at. `FacetRow` owns that.
  2. Counts say how many a person would get, so they are computed with the
     other dimensions applied and their own ignored. That is the caller's job.
  3. Empty means everything. Clearing the last value restores the full fleet
     rather than emptying it.

  Controlled, because the counts are derived from a selection the caller
  already owns. Each dimension hands back its own next selection.
-->
<script lang="ts">
  import { t } from '../../i18n';
  import FacetPopover from '../facet-popover/facet-popover.svelte';
  import { facetListSummary } from '../facet-popover/summary';
  import SelectFacet from '../select-facet/select-facet.svelte';
  import type { SelectFacetGroup } from '../select-facet/types';
  import type { PrinterFacetDimension, PrinterStateFilterProps } from './types';

  /** Past this many values in total the list gets a search box. */
  const SEARCHABLE_ABOVE = 8;

  let {
    severity,
    state,
    label,
    width = 240,
    size,
  }: PrinterStateFilterProps = $props();

  const facetLabel = $derived(
    label ?? t('primitives.printerStateFilter.state'),
  );

  const toggleIn = (selected: string[], value: string): string[] =>
    selected.includes(value)
      ? selected.filter((held) => held !== value)
      : [...selected, value];

  const asGroup = (
    id: string,
    heading: string,
    dimension: PrinterFacetDimension,
  ): SelectFacetGroup => {
    const labelFor = dimension.labelFor ?? ((value: string) => value);
    return {
      id,
      heading,
      selected: dimension.selected,
      onToggle: (value) => {
        dimension.onSelectedChange(toggleIn(dimension.selected, value));
      },
      options: dimension.values.map((value) => ({
        value,
        label: labelFor(value),
        count: dimension.counts.get(value) ?? 0,
      })),
    };
  };

  const groups = $derived([
    ...(severity === undefined
      ? []
      : [
          asGroup(
            'severity',
            t('primitives.printerStateFilter.severity'),
            severity,
          ),
        ]),
    asGroup('state', t('primitives.printerStateFilter.state'), state),
  ]);

  // The summary reads in labels and not raw values, because the trigger is
  // prose. It spans both dimensions, so a picked tier shows there as readily
  // as a picked state: the trigger answers what this list is narrowed by, and
  // that does not care which group a value came from.
  const picked = $derived([
    ...(severity?.selected ?? []).map(
      severity?.labelFor ?? ((value: string) => value),
    ),
    ...state.selected.map(state.labelFor ?? ((value: string) => value)),
  ]);

  const total = $derived((severity?.values.length ?? 0) + state.values.length);
</script>

<FacetPopover
  label={facetLabel}
  summary={facetListSummary(picked)}
  count={picked.length}
  {width}
  {size}
>
  <SelectFacet
    {groups}
    searchable={total > SEARCHABLE_ABOVE}
    searchPlaceholder={t('primitives.printerStateFilter.search')}
    searchAriaLabel={t('primitives.printerStateFilter.searchLabel')}
  />
</FacetPopover>
