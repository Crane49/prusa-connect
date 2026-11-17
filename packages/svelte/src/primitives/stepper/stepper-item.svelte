<script lang="ts">
  import CheckIcon from '@lucide/svelte/icons/check';

  import type { StepperItemProps } from './types';
  import { indicators, labels, spoken } from './variants';

  let { step, position, state, compact = false }: StepperItemProps = $props();
</script>

<!-- `aria-current="step"` is what a screen reader uses to find where the
     person is. The state word beside it is read out and never drawn, because
     the fill and the tick already say it on screen. -->
<li
  data-slot="stepper-item"
  data-state={state}
  aria-current={state === 'current' ? 'step' : undefined}
  class={[
    'flex min-w-0 items-center gap-2',
    compact ? 'shrink-0' : 'flex-1 flex-col gap-2 text-center',
  ]}
>
  <span
    data-slot="stepper-indicator"
    aria-hidden="true"
    class={[
      'rounded-panel duration-base ease-standard flex shrink-0 items-center justify-center border text-caption font-medium tabular-nums transition-colors motion-reduce:transition-none',
      indicators[state],
      compact ? 'size-6' : 'text-body size-8',
    ]}
  >
    <!-- The tick and the number both stay in the tree, and one of the two is
         hidden. A step change then writes attributes and adds no nodes, which
         is what keeps a guided dialog's Next and Back down to one frame. -->
    <CheckIcon
      class={[compact ? 'size-3' : 'size-4', state !== 'done' && 'hidden']}
    />
    <span class={[state === 'done' && 'hidden']}>{position}</span>
  </span>
  <span
    class={[
      'flex min-w-0 flex-col gap-0.5',
      compact ? 'sr-only' : 'items-center',
    ]}
  >
    <span
      data-slot="stepper-label"
      class={['text-body font-medium', labels[state]]}>{step.label}</span
    >
    {#if step.description && !compact}
      <!-- Drawn only where the row has the width for it. Below the `sm`
           container width the labels alone already fill the line. -->
      <span
        data-slot="stepper-description"
        class="hidden text-caption text-muted-foreground @sm/stepper:block"
        >{step.description}</span
      >
    {/if}
    <span class="sr-only">{spoken[state]}</span>
  </span>
</li>
