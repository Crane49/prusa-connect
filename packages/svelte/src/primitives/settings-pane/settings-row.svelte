<script lang="ts">
  import type { SettingsRowProps } from './types';

  let {
    label,
    description,
    control,
    id,
    class: className,
  }: SettingsRowProps = $props();
</script>

<!-- The label is text and not a `<label>` element: the control in the slot
     owns its own labelling, and a second `<label>` around it would give a
     screen reader two names for one field. -->
<div
  data-slot="settings-row"
  class={[
    'flex max-w-3xl flex-col gap-2 py-3 @md/pane:flex-row @md/pane:items-center @md/pane:justify-between @md/pane:gap-6',
    className,
  ]}
>
  <div class="flex min-w-0 flex-col gap-0.5">
    <span id={id ? `${id}-label` : undefined} class="text-body font-medium"
      >{label}</span
    >
    {#if description}
      <span
        id={id ? `${id}-description` : undefined}
        class="text-caption text-muted-foreground"
      >
        {description}
      </span>
    {/if}
  </div>
  {#if control}
    <div class="flex shrink-0 items-center gap-2">
      {@render control()}
    </div>
  {/if}
</div>
