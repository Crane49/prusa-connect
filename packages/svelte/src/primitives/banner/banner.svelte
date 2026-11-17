<!--
  A strip across the page for a standing condition. It sits flush with the
  page edges and announces itself as a status rather than an alert: it is not
  an interruption.
-->
<script lang="ts">
  import type { BannerProps } from './types';

  let {
    tone = 'warning',
    icon: Icon,
    children,
    actions,
    class: className,
  }: BannerProps = $props();
</script>

<div
  role="status"
  data-slot="banner"
  data-tone={tone}
  class={[
    'flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-warning bg-card px-4 py-3 text-sm text-warning-text @xl/app-shell:px-6',
    className,
  ]}
>
  {#if Icon}
    <Icon aria-hidden="true" class="size-4 shrink-0" />
  {/if}
  <div class="min-w-0 flex-1">{@render children()}</div>
  {#if actions}
    {@render actions()}
  {/if}
</div>
