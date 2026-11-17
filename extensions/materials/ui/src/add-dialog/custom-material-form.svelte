<!--
  The fields of a custom material, in four sections: what the material is, how
  it prints, what it is physically, and how it is classified. It sends what
  was typed to `onsave` and makes no request itself, so the add dialog and the
  Custom material dialog create with it and the material page edits with it:
  `initial` fills the fields and `submitLabel` names the button.

  The manufacturer, the name and the primary colour are required, and the type
  too unless the material is a resin; the button stays off until they are
  filled. A preset fills a filament's temperatures and density, all of them
  still editable. The name follows the type and the manufacturer until the
  person writes one of their own.

  A number left empty is left out, which the module keeps as unknown. A resin
  has no type and no temperatures, so none are sent for it.
-->
<script lang="ts">
  import PlusIcon from '@lucide/svelte/icons/plus';
  import {
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    Chip,
    ColorInput,
    DialogFooter,
    Field,
    Input,
    SwatchPicker,
  } from '@prusa/connect-ui/primitives';
  import { onMount, untrack } from 'svelte';

  import type { MaterialFields } from '../api';
  import { heldHost } from '../host';
  import { heldList } from '../list.svelte';
  import FaultNote from '../materials-page/fault-note.svelte';
  import SearchSelect from '../search-select.svelte';
  import {
    CERTIFICATIONS,
    MAX_SECONDARY,
    PALETTE,
    PRESETS,
    type PresetValues,
    TAGS,
  } from './custom-options';

  let {
    initial,
    submitLabel,
    busy,
    fault,
    onsave,
    oncancel,
  }: {
    initial?: MaterialFields | undefined;
    submitLabel: string;
    busy: boolean;
    fault: unknown;
    onsave: (fields: MaterialFields) => void;
    oncancel: () => void;
  } = $props();

  const t = heldHost().t;
  const list = heldList();

  type Numeric =
    | keyof PresetValues
    | 'transmission_distance'
    | 'hardness_shore_a'
    | 'hardness_shore_d';

  /** A filament's temperatures, two to a row; a resin has none of them. */
  const TEMPERATURES: readonly { key: Numeric; label: string }[] = [
    { key: 'min_print_temp', label: 'materials.custom.minPrint' },
    { key: 'max_print_temp', label: 'materials.custom.maxPrint' },
    { key: 'min_bed_temp', label: 'materials.custom.minBed' },
    { key: 'max_bed_temp', label: 'materials.custom.maxBed' },
    { key: 'preheat_temp', label: 'materials.custom.preheat' },
    { key: 'chamber_temp', label: 'materials.custom.chamber' },
    { key: 'min_chamber_temp', label: 'materials.custom.minChamber' },
    { key: 'max_chamber_temp', label: 'materials.custom.maxChamber' },
    { key: 'drying_temp', label: 'materials.custom.dryingTemp' },
    { key: 'drying_time_min', label: 'materials.custom.dryingTime' },
  ];
  const PHYSICAL: readonly {
    key: Numeric;
    label: string;
    filament?: true;
  }[] = [
    { key: 'density', label: 'materials.custom.density' },
    {
      key: 'min_nozzle_diameter',
      label: 'materials.custom.minNozzle',
      filament: true,
    },
    { key: 'transmission_distance', label: 'materials.custom.td' },
    { key: 'hardness_shore_a', label: 'materials.custom.shoreA' },
    { key: 'hardness_shore_d', label: 'materials.custom.shoreD' },
  ];
  /** What a resin is never sent: it has no temperatures and no nozzle. */
  const FILAMENT_ONLY = new Set<Numeric>([
    ...TEMPERATURES.map((n) => n.key),
    'min_nozzle_diameter',
  ]);
  /** What a preset writes, each one cleared when the preset gives none. */
  const PRESET_KEYS: readonly (keyof PresetValues)[] = [
    'density',
    'min_print_temp',
    'max_print_temp',
    'preheat_temp',
    'min_bed_temp',
    'max_bed_temp',
    'chamber_temp',
    'min_chamber_temp',
    'max_chamber_temp',
    'drying_temp',
    'drying_time_min',
    'min_nozzle_diameter',
  ];
  /** Whole numbers on the module's side; the rest take decimals. */
  const DECIMAL = new Set<Numeric>([
    'density',
    'min_nozzle_diameter',
    'transmission_distance',
    'hardness_shore_a',
    'hardness_shore_d',
  ]);
  const ALL: readonly Numeric[] = [...TEMPERATURES, ...PHYSICAL].map(
    (n) => n.key,
  );
  const TAGS_BY_NAME = $derived(
    TAGS.map((name) => ({ name, label: t(`materials.tag.${name}`) })).sort(
      (a, b) => a.label.localeCompare(b.label),
    ),
  );
  const PRIMARY_SWATCHES = PALETTE.map((hex) => ({
    key: hex,
    hex,
    label: hex,
  }));

  // The form is made new each time it is shown, so it starts from `initial`.
  const start = untrack(() => initial);
  let klass = $state<'FFF' | 'SLA'>(start?.class === 'SLA' ? 'SLA' : 'FFF');
  let preset = $state<string | undefined>(undefined);
  let brand = $state(start?.brand_name ?? '');
  let type = $state(start?.type ?? '');
  let name = $state(start?.name ?? '');
  let abbreviation = $state(start?.abbreviation ?? '');
  let primary = $state<string | undefined>(
    start?.color_rgba ? start.color_rgba.slice(0, 7) : undefined,
  );
  // The picker has no alpha, so an edit keeps the one the material had.
  const alpha = (start?.color_rgba ?? '').slice(7, 9);
  let secondary = $state<string[]>([...(start?.secondary_colors ?? [])]);
  let ral = $state(start?.ral_code ?? '');
  let certifications = $state<string[]>([...(start?.certifications ?? [])]);
  let tags = $state<string[]>([...(start?.tags ?? [])]);
  // The numbers as typed, so an emptied field is left out rather than sent as 0.
  const numbers = $state<Partial<Record<Numeric, string>>>(
    Object.fromEntries(
      ALL.map((key) => {
        const value = start?.[key];
        return [key, value === undefined ? '' : String(value)];
      }),
    ),
  );

  onMount(() => {
    void list.readFacets();
  });

  const resin = $derived(klass === 'SLA');

  // The name follows the type and the manufacturer, "PLA Acme", until the
  // person writes one of their own: a name that is not the last one made
  // here is theirs.
  let made = '';
  const composed = (): string => `${type} ${brand.trim()}`.trim();
  const follow = (): void => {
    if (name.trim() !== '' && name !== made) return;
    made = composed();
    name = made;
  };

  const typeOptions = $derived(
    [...new Set([...list.types, ...PRESETS.map((p) => p.type)])]
      .sort()
      .map((value) => ({ value, label: value })),
  );

  // Catalogue manufacturers while the typed one is not one of them: picking
  // one writes its name as the catalogue spells it.
  const suggestions = $derived.by(() => {
    const query = brand.trim().toLowerCase();
    if (
      query === '' ||
      list.brands.some((b) => b.name.toLowerCase() === query)
    ) {
      return [];
    }

    return list.brands
      .filter((b) => b.name.toLowerCase().includes(query))
      .slice(0, 5);
  });

  const missing = $derived(
    brand.trim() === '' ||
      name.trim() === '' ||
      primary === undefined ||
      (!resin && type === ''),
  );

  const applyPreset = (chosen: (typeof PRESETS)[number]): void => {
    preset = chosen.type;
    type = chosen.type;
    for (const key of PRESET_KEYS) {
      const value = chosen.values[key];
      numbers[key] = value === undefined ? '' : String(value);
    }
    follow();
  };

  const toggle = (items: string[], item: string): string[] =>
    items.includes(item) ? items.filter((i) => i !== item) : [...items, item];

  const typed = (event: Event): string =>
    (event.currentTarget as HTMLInputElement).value;

  const submit = (event: SubmitEvent): void => {
    event.preventDefault();
    if (missing || busy || primary === undefined) return;
    const kept = ALL.filter(
      (key) =>
        (numbers[key] ?? '') !== '' && !(resin && FILAMENT_ONLY.has(key)),
    );
    onsave({
      // A diameter the form does not ask keeps what the material had.
      ...(start?.diameter_um === undefined
        ? {}
        : { diameter_um: start.diameter_um }),
      ...Object.fromEntries(
        kept.map((key) => {
          const value = Number(numbers[key]);
          return [key, DECIMAL.has(key) ? value : Math.round(value)];
        }),
      ),
      name: name.trim(),
      class: klass,
      brand_name: brand.trim(),
      type: resin ? '' : type,
      abbreviation: abbreviation.trim(),
      color_rgba: `${primary}${alpha}`,
      secondary_colors: secondary,
      ral_code: ral.trim(),
      certifications,
      tags,
    });
  };

  const required = (label: string): string => `${label} *`;
</script>

{#snippet numberField(key: Numeric, label: string)}
  <Field
    label={t(label)}
    control={Input}
    controlProps={{
      name: key,
      type: 'number',
      inputmode: 'decimal',
      step: 'any',
      value: numbers[key] ?? '',
      oninput: (e: Event) => (numbers[key] = typed(e)),
    }}
  />
{/snippet}

{#snippet identity()}
  <div
    class="flex flex-wrap gap-2"
    role="group"
    aria-label={t('materials.filter.class')}
  >
    {#each ['FFF', 'SLA'] as const as chosen (chosen)}
      <Chip
        selected={klass === chosen}
        onclick={() => {
          klass = chosen;
          // A resin has no filament type, so neither the type nor a preset
          // built around one survives the switch.
          if (chosen === 'SLA') {
            type = '';
            preset = undefined;
          }
          follow();
        }}
      >
        {chosen}
      </Chip>
    {/each}
  </div>

  {#if !resin}
    <div class="flex flex-col gap-2">
      <p class="text-sm font-medium">{t('materials.custom.presets')}</p>
      <p class="text-xs text-muted-foreground">
        {t('materials.custom.presetsHint')}
      </p>
      <div
        class="flex flex-wrap gap-2"
        role="group"
        aria-label={t('materials.custom.presets')}
      >
        {#each PRESETS as p (p.type)}
          <Chip selected={preset === p.type} onclick={() => applyPreset(p)}>
            {p.type}
          </Chip>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Two fields to a row where the dialog has the width for it. -->
  <div class="pair grid gap-4">
    <div class="flex flex-col gap-2">
      <Field
        label={required(t('materials.custom.brand'))}
        control={Input}
        controlProps={{
          name: 'brand_name',
          autocomplete: 'off',
          placeholder: t('materials.custom.brandPlaceholder'),
          value: brand,
          oninput: (e: Event) => {
            brand = typed(e);
            follow();
          },
        }}
      />
      {#if suggestions.length > 0}
        <div
          class="flex flex-wrap gap-2"
          role="group"
          aria-label={t('materials.custom.brandSuggestions')}
        >
          {#each suggestions as suggestion (suggestion.uuid)}
            <Chip
              onclick={() => {
                brand = suggestion.name;
                follow();
              }}
            >
              {suggestion.name}
            </Chip>
          {/each}
        </div>
      {/if}
    </div>
    {#if !resin}
      <div class="flex flex-col gap-2">
        <p class="text-sm font-medium">
          {required(t('materials.custom.type'))}
        </p>
        <SearchSelect
          label={t('materials.custom.type')}
          allLabel={t('materials.custom.typePlaceholder')}
          options={typeOptions}
          value={type === '' ? undefined : type}
          onPick={(picked: string | undefined) => {
            type = picked ?? '';
            // A preset only stands for as long as its own type does.
            if (preset !== type) preset = undefined;
            follow();
          }}
        />
      </div>
    {/if}
    <Field
      label={required(t('materials.custom.name'))}
      control={Input}
      controlProps={{
        name: 'name',
        autocomplete: 'off',
        placeholder: composed() || t('materials.custom.namePlaceholder'),
        value: name,
        oninput: (e: Event) => (name = typed(e)),
      }}
    />
    <Field
      label={t('materials.custom.abbreviation')}
      control={Input}
      controlProps={{
        name: 'abbreviation',
        autocomplete: 'off',
        placeholder: type,
        value: abbreviation,
        oninput: (e: Event) => (abbreviation = typed(e)),
      }}
    />
  </div>

  <div class="flex flex-col gap-2">
    <p class="text-sm font-medium">
      {required(t('materials.custom.primary'))}
    </p>
    <div class="flex flex-wrap items-center gap-2">
      <SwatchPicker
        swatches={PRIMARY_SWATCHES}
        value={primary}
        label={t('materials.custom.primary')}
        size="md"
        onPick={(hex: string | undefined) => {
          if (hex !== undefined) primary = hex;
        }}
      />
      <ColorInput
        label={t('materials.custom.primaryCustom')}
        value={primary ?? '#000000'}
        oninput={(hex: string) => (primary = hex)}
      />
      {#if primary !== undefined}
        <Chip onclick={() => (primary = undefined)}>{primary} ✕</Chip>
      {/if}
    </div>
  </div>

  <div class="flex flex-col gap-2">
    <p class="text-sm font-medium">{t('materials.custom.secondary')}</p>
    <div class="flex flex-wrap items-center gap-2">
      {#each secondary as hex, index (index)}
        <ColorInput
          label={t('materials.custom.secondaryOne', { n: index + 1 })}
          value={hex.slice(0, 7)}
          oninput={(next: string) => (secondary[index] = next)}
        />
      {/each}
      {#if secondary.length < MAX_SECONDARY}
        <Chip onclick={() => (secondary = [...secondary, '#000000'])}>
          <PlusIcon aria-hidden="true" class="size-3" />
          {t('materials.custom.addColour')}
        </Chip>
      {/if}
      {#if secondary.length > 0}
        <Chip onclick={() => (secondary = secondary.slice(0, -1))}>
          {t('materials.custom.removeColour')}
        </Chip>
      {/if}
    </div>
  </div>

  <div class="pair grid gap-4">
    <Field
      label={t('materials.custom.ral')}
      control={Input}
      controlProps={{
        name: 'ral_code',
        autocomplete: 'off',
        value: ral,
        oninput: (e: Event) => (ral = typed(e)),
      }}
    />
  </div>
{/snippet}

{#snippet temperatures()}
  <div class="pair grid gap-4">
    {#each TEMPERATURES as n (n.key)}
      {@render numberField(n.key, n.label)}
    {/each}
  </div>
{/snippet}

{#snippet physical()}
  <div class="pair grid gap-4">
    {#each PHYSICAL.filter((n) => !(resin && n.filament)) as n (n.key)}
      {@render numberField(n.key, n.label)}
    {/each}
  </div>
{/snippet}

{#snippet classification()}
  <div class="flex flex-col gap-2">
    <p class="text-sm font-medium">{t('materials.custom.certifications')}</p>
    <div
      class="flex flex-wrap gap-2"
      role="group"
      aria-label={t('materials.custom.certifications')}
    >
      {#each CERTIFICATIONS as c (c.name)}
        <Chip
          selected={certifications.includes(c.name)}
          onclick={() => (certifications = toggle(certifications, c.name))}
        >
          {c.label}
        </Chip>
      {/each}
    </div>
  </div>
  <div class="flex flex-col gap-2">
    <p class="text-sm font-medium">{t('materials.details.tags')}</p>
    <div class="max-h-48 overflow-y-auto rounded-md border border-border p-2">
      <div
        class="flex flex-wrap gap-2"
        role="group"
        aria-label={t('materials.details.tags')}
      >
        {#each TAGS_BY_NAME as tag (tag.name)}
          <Chip
            selected={tags.includes(tag.name)}
            onclick={() => (tags = toggle(tags, tag.name))}
          >
            {tag.label}
          </Chip>
        {/each}
      </div>
    </div>
  </div>
{/snippet}

<form data-part="custom-material" class="flex flex-col gap-4" onsubmit={submit}>
  <FaultNote {fault} />
  <Card>
    <CardHeader>
      <CardTitle
        ><h3 class="contents">{t('materials.custom.identity')}</h3></CardTitle
      >
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      {@render identity()}
    </CardContent>
  </Card>
  {#if !resin}
    <Card>
      <CardHeader>
        <CardTitle
          ><h3 class="contents">
            {t('materials.custom.temperatures')}
          </h3></CardTitle
        >
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        {@render temperatures()}
      </CardContent>
    </Card>
  {/if}
  <Card>
    <CardHeader>
      <CardTitle
        ><h3 class="contents">{t('materials.custom.physical')}</h3></CardTitle
      >
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      {@render physical()}
    </CardContent>
  </Card>
  <Card>
    <CardHeader>
      <CardTitle
        ><h3 class="contents">
          {t('materials.custom.classification')}
        </h3></CardTitle
      >
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      {@render classification()}
    </CardContent>
  </Card>
  <DialogFooter>
    <Button type="button" variant="secondary" onclick={oncancel}>
      {t('materials.add.cancel')}
    </Button>
    <Button
      type="submit"
      disabled={missing || busy}
      title={missing ? t('materials.custom.incomplete') : undefined}
    >
      {submitLabel}
    </Button>
  </DialogFooter>
</form>

<style>
  .pair {
    grid-template-columns: repeat(
      auto-fit,
      minmax(calc(var(--spacing) * 48), 1fr)
    );
  }
</style>
