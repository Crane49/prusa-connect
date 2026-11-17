<!-- A group with a label beside every item, for the radio-group tests. -->
<script lang="ts">
  import RadioGroup from '../primitives/radio-group/radio-group.svelte';
  import RadioGroupItem from '../primitives/radio-group/radio-group-item.svelte';

  interface Choice {
    value: string;
    label: string;
    disabled?: boolean;
  }

  let {
    choices,
    value = $bindable(''),
    label,
    onValueChange = () => undefined,
  }: {
    choices: readonly Choice[];
    value?: string;
    label: string;
    onValueChange?: (value: string) => void;
  } = $props();
</script>

<RadioGroup bind:value aria-label={label} {onValueChange}>
  {#each choices as choice (choice.value)}
    <div>
      <RadioGroupItem
        value={choice.value}
        id={choice.value}
        disabled={choice.disabled}
      />
      <label for={choice.value}>{choice.label}</label>
    </div>
  {/each}
</RadioGroup>
