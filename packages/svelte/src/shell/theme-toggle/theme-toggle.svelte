<script lang="ts">
  import MonitorIcon from '@lucide/svelte/icons/monitor';
  import MoonIcon from '@lucide/svelte/icons/moon';
  import SunIcon from '@lucide/svelte/icons/sun';

  import { t } from '../../i18n';
  import { Button } from '../../primitives';
  import { theme, type ThemePreference } from '../../styles/theme.svelte';

  const NEXT: Record<ThemePreference, ThemePreference> = {
    light: 'dark',
    dark: 'system',
    system: 'light',
  };

  const ICON = {
    light: SunIcon,
    dark: MoonIcon,
    system: MonitorIcon,
  };

  const NAME: Record<ThemePreference, string> = {
    light: 'shell.theme.light',
    dark: 'shell.theme.dark',
    system: 'shell.theme.system',
  };

  const Icon = $derived(ICON[theme.preference]);
  const label = $derived(
    `${t('shell.theme.label')}: ${t(NAME[theme.preference])}`,
  );
</script>

<Button
  variant="ghost"
  size="icon-md"
  {label}
  onclick={() => {
    theme.preference = NEXT[theme.preference];
  }}
>
  <Icon aria-hidden="true" />
</Button>
