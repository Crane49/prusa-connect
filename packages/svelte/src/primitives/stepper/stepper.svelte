<script lang="ts">
  import { t } from '../../i18n';
  import StepperItem from './stepper-item.svelte';
  import type { StepperProps, StepperState } from './types';

  let {
    steps,
    current,
    compact = false,
    label,
    class: className,
  }: StepperProps = $props();

  // A step out of range still has to draw something, so the index is clamped
  // rather than trusted. A caller counting from one would otherwise leave
  // every step upcoming and say nothing about it. An empty list clamps to zero
  // rather than to minus one, which would read as "step 0 of 0".
  const at = $derived(
    steps.length === 0 ? 0 : Math.min(Math.max(current, 0), steps.length - 1),
  );

  // A caller may name its own words. The default is read from the catalogue
  // when the component draws, so a change of language rewrites it
  // (frontend-i18n.md rule 1).
  const labelText = $derived(label ?? t('primitives.stepper.label'));

  const stateOf = (index: number): StepperState => {
    if (index < at) return 'done';
    if (index === at) return 'current';
    return 'upcoming';
  };
</script>

<!-- The container the items read. A step's description is drawn only where the
     row is wide enough for it, and that width is the region the stepper sits
     in and never the viewport. -->
<div
  data-slot="stepper"
  data-variant={compact ? 'compact' : 'full'}
  class="@container/stepper"
>
  {#if compact && steps.length > 0}
    <!-- The line a dialog header shows. It is the only place the count is
         written out, and a screen reader hears it before the numbers. -->
    <p class="mbe-2 text-caption font-medium text-muted-foreground">
      {t('primitives.stepper.step', { at: at + 1, count: steps.length })}
    </p>
  {/if}
  <ol
    aria-label={labelText}
    class={[
      'flex list-none items-start gap-2 p-0',
      compact ? 'flex-wrap' : 'gap-4',
      className,
    ]}
  >
    {#each steps as step, index (step.id)}
      <StepperItem
        {step}
        position={index + 1}
        state={stateOf(index)}
        {compact}
      />
    {/each}
  </ol>
</div>
