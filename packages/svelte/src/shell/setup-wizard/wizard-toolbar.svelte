<!-- The language and the colours of the wizard itself, changeable at any step. -->
<script lang="ts">
  import MonitorIcon from '@lucide/svelte/icons/monitor';
  import MoonIcon from '@lucide/svelte/icons/moon';
  import SunIcon from '@lucide/svelte/icons/sun';

  import { locale, LOCALES, t } from '../../i18n';
  import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    ToggleGroup,
    ToggleGroupItem,
  } from '../../primitives';
  import {
    theme,
    THEME_PREFERENCES,
    type ThemePreference,
  } from '../../styles/theme.svelte';

  let { onlanguage }: { onlanguage: (tag: string) => void } = $props();

  // A language is named in itself, which is how a person finds their own in
  // a list they cannot read yet.
  const nameOf = (tag: string): string => {
    try {
      const name = new Intl.DisplayNames([tag], { type: 'language' }).of(tag);

      return name === undefined
        ? tag
        : name.charAt(0).toLocaleUpperCase(tag) + name.slice(1);
    } catch {
      return tag;
    }
  };

  const languages = $derived(
    LOCALES.map((tag) => ({ value: tag, label: nameOf(tag) })),
  );

  const THEME_ICON = { light: SunIcon, dark: MoonIcon, system: MonitorIcon };
  const THEME_NAME = $derived({
    light: t('setupWizard.theme.light'),
    dark: t('setupWizard.theme.dark'),
    system: t('setupWizard.theme.system'),
  });
</script>

<div
  class="flex flex-wrap items-center justify-end gap-2"
  data-part="wizard-toolbar"
>
  <Select
    value={locale.current}
    items={languages}
    onValueChange={(next: string) => {
      onlanguage(next);
    }}
  >
    <SelectTrigger
      size="sm"
      class="min-w-32"
      aria-label={t('setupWizard.language.label')}
    >
      <SelectValue placeholder={t('setupWizard.language.label')} />
    </SelectTrigger>
    <SelectContent>
      {#each languages as language (language.value)}
        <SelectItem value={language.value}>{language.label}</SelectItem>
      {/each}
    </SelectContent>
  </Select>
  <ToggleGroup
    type="single"
    variant="outline"
    size="sm"
    value={theme.preference}
    onValueChange={(next: string) => {
      if (next !== '') theme.preference = next as ThemePreference;
    }}
    aria-label={t('setupWizard.theme.label')}
  >
    {#each THEME_PREFERENCES as name (name)}
      {@const Icon = THEME_ICON[name]}
      <ToggleGroupItem value={name} aria-label={THEME_NAME[name]}>
        <Icon aria-hidden="true" />
      </ToggleGroupItem>
    {/each}
  </ToggleGroup>
</div>
