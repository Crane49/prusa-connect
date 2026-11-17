<script lang="ts">
  import WifiIcon from '@lucide/svelte/icons/wifi';
  import WifiOffIcon from '@lucide/svelte/icons/wifi-off';

  import { t } from '../../i18n';
  import { Button } from '../../primitives';
  import type { ConnectionStatusProps } from './types';

  let {
    state,
    presentation = 'badge',
    class: className,
  }: ConnectionStatusProps = $props();

  const label = $derived(
    state === 'online' ? t('shell.online') : t('shell.offline'),
  );

  const Glyph = $derived(state === 'online' ? WifiIcon : WifiOffIcon);

  const dotClass = $derived([
    'size-2 shrink-0 rounded-full',
    state === 'online' ? 'bg-success' : 'bg-destructive',
  ]);
</script>

{#if presentation === 'dot'}
  <span
    aria-hidden="true"
    data-slot="connection-status"
    data-state={state}
    class={[dotClass, className]}
  ></span>
{:else}
  <Button
    variant="ghost"
    size="icon-sm"
    {label}
    data-slot="connection-status"
    data-state={state}
    class={className}
  >
    <Glyph
      aria-hidden="true"
      class={[
        'size-4',
        state === 'online' ? 'text-success' : 'text-destructive',
      ]}
    />
  </Button>
{/if}
