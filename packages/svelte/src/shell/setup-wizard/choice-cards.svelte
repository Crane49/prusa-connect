<!--
  One choice out of a few, each a card with a title and a sentence. The whole
  card is the label, so a click anywhere on it picks the option.
-->
<script lang="ts" generics="Value extends string">
  import type { Component } from 'svelte';

  import { t } from '../../i18n';
  import { Badge, RadioGroup, RadioGroupItem } from '../../primitives';

  interface ChoiceOption {
    value: Value;
    title: string;
    note: string;
    icon?: Component<{ class?: string; 'aria-hidden'?: 'true' }>;
    recommended?: boolean;
  }

  interface ChoiceCardsProps {
    label: string;
    value: Value | undefined;
    options: readonly ChoiceOption[];
    onchange: (value: Value) => void;
  }

  let { label, value, options, onchange }: ChoiceCardsProps = $props();

  const id = $props.id();
</script>

<RadioGroup
  class="grid gap-3"
  value={value ?? ''}
  aria-label={label}
  onValueChange={(next: string) => {
    const picked = options.find((option) => option.value === next);
    if (picked !== undefined) onchange(picked.value);
  }}
>
  {#each options as option (option.value)}
    {@const checked = option.value === value}
    <!-- eslint-disable-next-line svelte/no-restricted-html-elements -- a native radio inside its card label: radio-group semantics and arrow keys no primitive gives -->
    <label
      for={`${id}-${option.value}`}
      data-part="choice"
      data-checked={checked}
      class={[
        'flex cursor-pointer items-start gap-4 rounded-md border p-4 transition-colors motion-reduce:transition-none',
        checked
          ? 'border-selected bg-selected/10'
          : 'border-border hover:border-primary/60 hover:bg-accent',
      ]}
    >
      <RadioGroupItem
        value={option.value}
        id={`${id}-${option.value}`}
        class="mt-1"
      />
      {#if option.icon !== undefined}
        {@const Icon = option.icon}
        <span
          class={[
            'flex size-10 shrink-0 items-center justify-center rounded-md',
            checked
              ? 'bg-primary text-primary-foreground'
              : 'bg-muted text-muted-foreground',
          ]}
        >
          <Icon class="size-5" aria-hidden="true" />
        </span>
      {/if}
      <span class="flex min-w-0 flex-col gap-1">
        <span class="flex flex-wrap items-center gap-2 font-medium">
          {option.title}
          {#if option.recommended === true}
            <Badge variant="secondary"
              >{t('setupWizard.path.recommended')}</Badge
            >
          {/if}
        </span>
        <span class="text-sm text-muted-foreground">{option.note}</span>
      </span>
    </label>
  {/each}
</RadioGroup>
