<!-- The appearance room: how this app is drawn, and where it opens. -->
<script lang="ts">
  import MonitorIcon from '@lucide/svelte/icons/monitor';
  import MoonIcon from '@lucide/svelte/icons/moon';
  import SunIcon from '@lucide/svelte/icons/sun';

  import { type HomePreference, homeOn } from '../../domain';
  import {
    type Clock,
    CLOCKS,
    formatDateTime,
    formatNumber,
    formatting,
    formatUnit,
    t,
    TEMPERATURE_UNITS,
    type TemperatureUnit,
  } from '../../i18n';
  import {
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
    DEFAULT_VIEW_FIELDS,
    type DefaultViewField,
    Label,
    PAGE_SIZES,
    type PageSize,
    RadioGroup,
    RadioGroupItem,
    Separator,
    SettingsPane,
    ToggleGroup,
    ToggleGroupItem,
    type ViewModeName,
    viewModes,
    viewPreferences,
  } from '../../primitives';
  import { type Skin, SKIN_NAMES } from '../../styles/skin';
  import { skin } from '../../styles/skin-store.svelte';
  import { theme, type ThemePreference } from '../../styles/theme.svelte';
  import type { SettingsAppearanceScreenProps } from './types';
  import ViewSchematic from './view-schematic.svelte';

  let { home, dashboards = true }: SettingsAppearanceScreenProps = $props();

  /** What the folded row says it is hiding: the picks themselves, not their count. */
  const defaultViewsSummary = $derived(
    DEFAULT_VIEW_FIELDS.map((field) => {
      const picked = viewModes([...field.modes]).find(
        (option) => option.value === chosenView(field),
      );

      return `${t(field.labelKey)}: ${picked?.label ?? '\u2014'}`;
    }).join(' \u00b7 '),
  );

  const THEME_ICON = {
    system: MonitorIcon,
    light: SunIcon,
    dark: MoonIcon,
  } as const;

  // The order the reference reads in.
  const THEMES: readonly ThemePreference[] = ['system', 'light', 'dark'];

  const HOMES: readonly HomePreference[] = [
    'dashboard',
    'auto',
    'my-printer',
    'all-printers',
  ];

  // Every key is written out rather than assembled from a value.
  const THEME_NAME = {
    system: 'settingsAppearance.theme.system',
    light: 'settingsAppearance.theme.light',
    dark: 'settingsAppearance.theme.dark',
  } as const;

  // A key per value rather than one sentence per group.
  const THEME_HINT = {
    system: 'settingsAppearance.theme.hint.system',
    light: 'settingsAppearance.theme.hint.light',
    dark: 'settingsAppearance.theme.hint.dark',
  } as const;

  const SKIN_HINT = {
    prusa: 'settingsAppearance.skin.hint.prusa',
    slicer: 'settingsAppearance.skin.hint.slicer',
    prusapro: 'settingsAppearance.skin.hint.prusapro',
  } as const;

  const HOME_NAME = {
    dashboard: 'settingsAppearance.home.dashboard',
    auto: 'settingsAppearance.home.auto',
    'my-printer': 'settingsAppearance.home.myPrinter',
    'all-printers': 'settingsAppearance.home.allPrinters',
  } as const;

  const HOME_NOTE = {
    dashboard: 'settingsAppearance.home.dashboardNote',
    auto: 'settingsAppearance.home.autoNote',
    'my-printer': 'settingsAppearance.home.myPrinterNote',
    'all-printers': 'settingsAppearance.home.allPrintersNote',
  } as const;

  const SKIN_NAME = {
    prusa: 'settingsAppearance.skin.prusa',
    slicer: 'settingsAppearance.skin.slicer',
    prusapro: 'settingsAppearance.skin.prusapro',
  } as const;

  const CLOCK_NAME = {
    '24-hour': 'settingsAppearance.clock.24',
    '12-hour': 'settingsAppearance.clock.12',
  } as const;

  const TEMPERATURE_NAME = {
    celsius: 'settingsAppearance.temperature.celsius',
    fahrenheit: 'settingsAppearance.temperature.fahrenheit',
  } as const;

  // A sample rather than a sentence naming the pattern.
  const SAMPLE_INSTANT = new Date(2026, 7, 19, 17, 5);

  // A nozzle at its usual PLA target.
  const SAMPLE_DEGREES = 215;

  const id = $props.id();

  let viewsOpen = $state(false);

  /** The stored pick, or the shape the listing opens in until there is one. */
  const chosenView = (field: DefaultViewField): string =>
    viewPreferences.defaultView(field.id) ?? field.initial;

  // Both setters reject a word they do not know.
  const chooseTheme = (next: string): void => {
    theme.preference = next as ThemePreference;
  };

  const chooseSkin = (next: string): void => {
    skin.preference = next as Skin;
  };

  const chooseClock = (next: string): void => {
    formatting.clock = next as Clock;
  };

  const chooseTemperature = (next: string): void => {
    formatting.temperature = next as TemperatureUnit;
  };

  // The store rejects a size no pager offers.
  const chooseRows = (next: string): void => {
    viewPreferences.rowsPerPage = Number(next) as PageSize;
  };

  // The port takes the three words and nothing else.
  // A server without dashboards does not offer them as the home page.
  const homes = $derived(
    dashboards ? HOMES : HOMES.filter((name) => name !== 'dashboard'),
  );
  const homeShown = $derived(homeOn(home.home, dashboards));

  const isHome = (value: string): value is HomePreference =>
    (homes as readonly string[]).includes(value);

  const chooseHome = (next: string): void => {
    if (isHome(next)) home.setHome(next);
  };
</script>

<!-- The thumbnail is decoration. -->
{#snippet layout(shape: HomePreference)}
  <span
    aria-hidden="true"
    class="rounded-input grid h-12 w-20 shrink-0 grid-cols-2 gap-1 border border-border bg-muted p-1.5"
  >
    {#if shape === 'dashboard'}
      <span class="rounded-badge col-span-2 bg-primary"></span>
      <span class="rounded-badge bg-border"></span>
      <span class="rounded-badge bg-border"></span>
    {:else if shape === 'auto'}
      <span class="rounded-badge row-span-2 bg-primary"></span>
      <span class="rounded-badge bg-border"></span>
      <span class="rounded-badge bg-border"></span>
    {:else if shape === 'my-printer'}
      <span class="rounded-badge col-span-2 row-span-2 bg-primary"></span>
    {:else}
      <span class="rounded-badge bg-border"></span>
      <span class="rounded-badge bg-border"></span>
      <span class="rounded-badge bg-border"></span>
      <span class="rounded-badge bg-border"></span>
    {/if}
  </span>
{/snippet}

<SettingsPane class="w-full" title={t('settingsAppearance.title')}>
  <div class="flex flex-col gap-4 pb-6">
    <h3 class="text-xs font-normal text-muted-foreground">
      {t('settingsAppearance.look.title')}
    </h3>

    <div class="grid gap-6 @[44rem]/pane:grid-cols-2">
      <div class="flex flex-col gap-2">
        <h4 class="text-body font-medium">
          {t('settingsAppearance.theme.label')}
        </h4>
        <p class="text-caption text-muted-foreground">
          {t('settingsAppearance.theme.note')}
        </p>
        <ToggleGroup
          type="single"
          variant="outline"
          value={theme.preference}
          onValueChange={chooseTheme}
          aria-label={t('settingsAppearance.theme.label')}
        >
          {#each THEMES as name (name)}
            {@const Icon = THEME_ICON[name]}
            <ToggleGroupItem value={name}>
              <Icon />
              {t(THEME_NAME[name])}
            </ToggleGroupItem>
          {/each}
        </ToggleGroup>
        <p class="text-caption text-muted-foreground">
          {t(THEME_HINT[theme.preference])}
        </p>
      </div>

      <div class="flex flex-col gap-2">
        <h4 class="text-body font-medium">
          {t('settingsAppearance.skin.label')}
        </h4>
        <p class="text-caption text-muted-foreground">
          {t('settingsAppearance.skin.note')}
        </p>
        <ToggleGroup
          type="single"
          variant="outline"
          value={skin.preference}
          onValueChange={chooseSkin}
          aria-label={t('settingsAppearance.skin.label')}
        >
          {#each SKIN_NAMES as name (name)}
            <ToggleGroupItem value={name}>
              {t(SKIN_NAME[name])}
            </ToggleGroupItem>
          {/each}
        </ToggleGroup>
        <p class="text-caption text-muted-foreground">
          {t(SKIN_HINT[skin.preference])}
        </p>
      </div>
    </div>
  </div>

  <Separator />

  <div class="flex flex-col gap-4 py-6">
    <h3 class="text-xs font-normal text-muted-foreground">
      {t('settingsAppearance.reading.title')}
    </h3>

    <div class="grid gap-6 @[44rem]/pane:grid-cols-2">
      <div class="flex flex-col gap-2">
        <h4 class="text-body font-medium">
          {t('settingsAppearance.clock.label')}
        </h4>
        <p class="text-caption text-muted-foreground">
          {t('settingsAppearance.clock.note')}
        </p>
        <ToggleGroup
          type="single"
          variant="outline"
          value={formatting.clock}
          onValueChange={chooseClock}
          aria-label={t('settingsAppearance.clock.label')}
        >
          {#each CLOCKS as name (name)}
            <ToggleGroupItem value={name}>
              {t(CLOCK_NAME[name])}
            </ToggleGroupItem>
          {/each}
        </ToggleGroup>
        <p class="text-caption text-muted-foreground">
          {formatDateTime(SAMPLE_INSTANT)}
        </p>
      </div>

      <div class="flex flex-col gap-2">
        <h4 class="text-body font-medium">
          {t('settingsAppearance.temperature.label')}
        </h4>
        <p class="text-caption text-muted-foreground">
          {t('settingsAppearance.temperature.note')}
        </p>
        <ToggleGroup
          type="single"
          variant="outline"
          value={formatting.temperature}
          onValueChange={chooseTemperature}
          aria-label={t('settingsAppearance.temperature.label')}
        >
          {#each TEMPERATURE_UNITS as name (name)}
            <ToggleGroupItem value={name}>
              {t(TEMPERATURE_NAME[name])}
            </ToggleGroupItem>
          {/each}
        </ToggleGroup>
        <p class="text-caption text-muted-foreground">
          {formatUnit(SAMPLE_DEGREES, '°C', { maximumFractionDigits: 0 })}
        </p>
      </div>

      <div class="flex flex-col gap-2">
        <h4 class="text-body font-medium">
          {t('settingsAppearance.rows.label')}
        </h4>
        <p class="text-caption text-muted-foreground">
          {t('settingsAppearance.rows.note')}
        </p>
        <!-- A toggle group and not the pager's own select. -->
        <ToggleGroup
          type="single"
          variant="outline"
          value={String(viewPreferences.rowsPerPage)}
          onValueChange={chooseRows}
          aria-label={t('settingsAppearance.rows.label')}
        >
          {#each PAGE_SIZES as size (size)}
            <ToggleGroupItem value={String(size)}>
              {formatNumber(size)}
            </ToggleGroupItem>
          {/each}
        </ToggleGroup>
      </div>
    </div>
  </div>

  <Separator />

  <div class="flex flex-col gap-4 pt-6">
    <h3 class="text-xs font-normal text-muted-foreground">
      {t('settingsAppearance.open.title')}
    </h3>

    <div class="grid gap-6 @[44rem]/pane:grid-cols-2">
      <div class="flex flex-col gap-2">
        <h4 class="text-body font-medium">
          {t('settingsAppearance.home.label')}
        </h4>
        <p class="text-caption text-muted-foreground">
          {t('settingsAppearance.home.note')}
        </p>
        <RadioGroup
          class="gap-4 pt-1"
          value={homeShown}
          onValueChange={chooseHome}
          aria-label={t('settingsAppearance.home.label')}
        >
          {#each homes as name (name)}
            <div class="flex items-start gap-3">
              <RadioGroupItem value={name} id={`${id}-${name}`} class="mt-1" />
              {@render layout(name)}
              <div class="flex min-w-0 flex-col gap-0.5">
                <Label variant="option" for={`${id}-${name}`}>
                  {t(HOME_NAME[name])}
                </Label>
                <span class="text-caption text-muted-foreground">
                  {t(HOME_NOTE[name])}
                </span>
              </div>
            </div>
          {/each}
        </RadioGroup>
      </div>
    </div>

    <!-- Folded: a person opens Appearance to change the theme far more often
         than to decide which shape Users opens in, and the closed row still
         says every current pick, so nothing is hidden, only stowed. -->
    <Collapsible class="mt-2" bind:open={viewsOpen}>
      <CollapsibleTrigger variant="row">
        <span class="flex min-w-0 flex-1 flex-col">
          <span class="text-body font-medium">
            {t('settingsAppearance.defaultViews.label')}
          </span>
          <span class="truncate text-caption font-normal text-muted-foreground">
            {defaultViewsSummary}
          </span>
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <p class="pt-3 text-caption text-muted-foreground">
          {t('settingsAppearance.defaultViews.note')}
        </p>
        <div
          class="grid grid-cols-1 gap-x-8 gap-y-4 pt-3 @[36rem]/pane:grid-cols-2 @[60rem]/pane:grid-cols-3"
        >
          {#each DEFAULT_VIEW_FIELDS as field (field.id)}
            <div class="flex flex-col gap-2">
              <h4 id={`${id}-view-${field.id}`} class="text-body font-medium">
                {t(field.labelKey)}
              </h4>
              <!-- `flex` as well as `flex-row`: the primitive's own base is a
                   grid, which a bare direction does not turn off. -->
              <RadioGroup
                class="flex flex-row gap-2"
                value={chosenView(field)}
                onValueChange={(next: string) => {
                  viewPreferences.setDefaultView(field.id, next);
                }}
                aria-labelledby={`${id}-view-${field.id}`}
              >
                <!-- `viewModes()` and not a list written here: the names, the
                     words and the order are the listing's own. -->
                {#each viewModes([...field.modes]) as option (option.value)}
                  {@const chosen = option.value === chosenView(field)}
                  <!-- eslint-disable-next-line svelte/no-restricted-html-elements -- a native radio inside its card label: radio-group semantics and arrow keys no primitive gives -->
                  <label
                    for={`${id}-view-${field.id}-${option.value}`}
                    class={[
                      'flex cursor-pointer flex-col items-center gap-1.5 rounded-md border p-2 text-caption font-medium transition-colors focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background hover:bg-accent',
                      chosen
                        ? 'border-selected bg-selected/10 text-foreground'
                        : 'border-input text-muted-foreground',
                    ]}
                  >
                    <RadioGroupItem
                      value={option.value}
                      id={`${id}-view-${field.id}-${option.value}`}
                      class="sr-only"
                    />
                    <ViewSchematic kind={option.value as ViewModeName} />
                    {option.label}
                  </label>
                {/each}
              </RadioGroup>
            </div>
          {/each}
        </div>
      </CollapsibleContent>
    </Collapsible>
  </div>
</SettingsPane>
