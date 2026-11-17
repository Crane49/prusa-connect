<!--
  The steps of a wizard down its side: done, current and still ahead, each a
  round marker with the step's name and an optional line under it.

  With `onpick` a passed step is a button that opens it again, and the list is
  a navigation region. A step ahead that the person has already reached, up
  to `reached`, is a button too, and keeps its number. Without it the list only says where the person is, so
  it is a plain ordered list and nothing in it takes the focus.
-->
<script lang="ts">
  import CheckIcon from '@lucide/svelte/icons/check';

  import Button from '../button/button.svelte';
  import type { StepListProps, StepListState } from './types';
  import { labels, lines, markers } from './variants';

  let {
    label,
    steps,
    current,
    reached,
    onpick,
    connected = false,
    class: className,
  }: StepListProps = $props();

  const stateOf = (position: number): StepListState => {
    if (position < current) return 'done';
    if (position === current) return 'current';
    return 'upcoming';
  };
</script>

{#snippet marker(state: StepListState, position: number)}
  <span
    data-slot="step-list-marker"
    aria-hidden="true"
    class={[
      'flex size-6 shrink-0 items-center justify-center rounded-full border text-xs',
      markers[state],
    ]}
  >
    {#if state === 'done'}
      <CheckIcon class="size-3.5" aria-hidden="true" />
    {:else}
      {position + 1}
    {/if}
  </span>
{/snippet}

{#snippet text(step: StepListProps['steps'][number])}
  <span class="flex min-w-0 flex-col gap-0.5">
    <span class="truncate">{step.label}</span>
    {#if step.description !== undefined}
      <span
        data-slot="step-list-description"
        class="text-xs font-normal text-muted-foreground"
      >
        {step.description}
      </span>
    {/if}
  </span>
{/snippet}

{#if onpick === undefined}
  <ol
    data-slot="step-list"
    aria-label={label}
    class={['flex flex-col', className]}
  >
    {#each steps as step, position (step.id)}
      {@const state = stateOf(position)}
      <li
        data-slot="step-list-item"
        data-state={state}
        aria-current={state === 'current' ? 'step' : undefined}
        class={[
          'relative flex items-start gap-3 text-sm',
          connected && position < steps.length - 1 && 'pbe-6',
          labels[state],
        ]}
      >
        {#if connected && position < steps.length - 1}
          <!-- From under this marker to just above the next one. The marker
               is size-6, so start-3 is its middle. -->
          <span
            data-slot="step-list-line"
            aria-hidden="true"
            class={['absolute start-3 top-7 bottom-1 w-px', lines[state]]}
          ></span>
        {/if}
        {@render marker(state, position)}
        <span class="pt-0.5">{@render text(step)}</span>
      </li>
    {/each}
  </ol>
{:else}
  <nav data-slot="step-list" aria-label={label} class={className}>
    <ol class="flex flex-col gap-1">
      {#each steps as step, position (step.id)}
        {@const state = stateOf(position)}
        {@const ahead =
          state === 'upcoming' && position <= (reached ?? current)}
        <li data-slot="step-list-item" data-state={state}>
          <Button
            variant="ghost"
            disabled={state !== 'done' && !ahead}
            aria-current={state === 'current' ? 'step' : undefined}
            onclick={() => onpick(step.id)}
            class={[
              'h-auto w-full justify-start gap-3 px-2 py-1.5 text-start text-sm disabled:opacity-100',
              state === 'current' &&
                'bg-background font-medium text-foreground',
              (state === 'done' || ahead) &&
                'text-foreground hover:bg-background/60',
              state === 'upcoming' && !ahead && 'text-muted-foreground',
            ]}
          >
            {@render marker(state, position)}
            {@render text(step)}
          </Button>
        </li>
      {/each}
    </ol>
  </nav>
{/if}
