<script lang="ts">
  import CheckIcon from '@lucide/svelte/icons/check';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import type { GuidedEndProps } from './types';
  import { endTiles } from './variants';

  let { status, title, message, help }: GuidedEndProps = $props();
</script>

<!-- A failure is an alert, because it arrives while the person is reading
     something else. A success is a status, which waits its turn. -->
<div
  data-slot="guided-end"
  data-variant={status}
  role={status === 'failure' ? 'alert' : 'status'}
  class="flex items-start gap-3"
>
  <span
    aria-hidden="true"
    class={[
      'rounded-panel flex size-10 shrink-0 items-center justify-center',
      endTiles[status],
    ]}
  >
    {#if status === 'success'}
      <CheckIcon class="size-6" />
    {:else}
      <TriangleAlertIcon class="size-6" />
    {/if}
  </span>
  <div class="flex min-w-0 flex-col gap-1">
    {#if title}
      <p class="text-lead font-semibold">{title}</p>
    {/if}
    {#if message}
      <p class="text-body text-muted-foreground">{message}</p>
    {/if}
    {@render help?.()}
  </div>
</div>
