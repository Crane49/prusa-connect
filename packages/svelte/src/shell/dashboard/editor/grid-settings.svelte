<!--
  The settings of the grid itself, behind a button in the edit bar: how many
  columns it has, how dense its rows are, and who sees the dashboard.
-->
<script lang="ts">
  import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';

  import type { DashboardDensity, DashboardVisibility } from '../../../domain';
  import { formatNumber, t } from '../../../i18n';
  import {
    Button,
    Label,
    Popover,
    PopoverContent,
    PopoverTrigger,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
  } from '../../../primitives';
  import { dashboardContext } from '../context';
  import Choice from '../settings/choice.svelte';

  const { dashboards, editor } = dashboardContext();
  const uid = $props.id();

  /** The widths a grid is offered at. */
  const COLUMNS = ['6', '8', '12', '16', '24'] as const;

  const DENSITIES: readonly { value: DashboardDensity; key: string }[] = [
    { value: 'compact', key: 'dashboard.density.compact' },
    { value: 'comfortable', key: 'dashboard.density.comfortable' },
    { value: 'spacious', key: 'dashboard.density.spacious' },
  ];

  const VISIBILITIES: readonly { value: DashboardVisibility; key: string }[] = [
    { value: 'everyone', key: 'dashboard.visibility.everyone' },
    { value: 'operators', key: 'dashboard.visibility.operators' },
    { value: 'admins', key: 'dashboard.visibility.admins' },
  ];

  const draft = $derived(editor.draft);
  // Only a person who may configure shows a dashboard to administrators only,
  // and the default dashboard stays one everybody sees (ADR-20261008-sww rule 9).
  const visibilities = $derived(
    VISIBILITIES.filter(
      (option) =>
        (option.value !== 'admins' ||
          dashboards.rights.configure ||
          draft?.visibility === 'admins') &&
        (option.value === 'everyone' ||
          editor.dashboard !== dashboards.defaultId),
    ),
  );
  // A grid saved at a width this list does not offer, such as 10, keeps its
  // own entry, so the select never shows an empty box.
  const columnItems = $derived(
    [...new Set([...COLUMNS.map(Number), draft?.columns ?? 12])]
      .sort((a, b) => a - b)
      .map((value) => ({ value: String(value), label: formatNumber(value) })),
  );
</script>

{#if draft !== undefined}
  <Popover>
    <PopoverTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="ghost" data-part="grid-settings">
          <LayoutGridIcon aria-hidden="true" />
          {t('dashboard.edit.grid')}
        </Button>
      {/snippet}
    </PopoverTrigger>
    <PopoverContent align="end" class="grid w-80 gap-4">
      <div class="grid gap-2">
        <Label for="{uid}-columns">{t('dashboard.edit.columns')}</Label>
        <Select
          value={String(draft.columns)}
          items={columnItems}
          onValueChange={(next: string) => {
            editor.setColumns(Number(next));
          }}
        >
          <SelectTrigger id="{uid}-columns">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {#each columnItems as item (item.value)}
              <SelectItem value={item.value} label={item.label}
                >{item.label}</SelectItem
              >
            {/each}
          </SelectContent>
        </Select>
        <p class="text-xs text-muted-foreground">
          {t('dashboard.edit.columnsNote')}
        </p>
      </div>
      <Choice
        label={t('dashboard.edit.density')}
        value={draft.density}
        options={DENSITIES.map((density) => ({
          value: density.value,
          label: t(density.key),
        }))}
        onchange={(density: DashboardDensity) => {
          editor.setDensity(density);
        }}
      />
      <Choice
        label={t('dashboard.edit.visibility')}
        value={draft.visibility}
        options={visibilities.map((option) => ({
          value: option.value,
          label: t(option.key),
        }))}
        onchange={(visibility: DashboardVisibility) => {
          editor.setVisibility(visibility);
        }}
      />
    </PopoverContent>
  </Popover>
{/if}
