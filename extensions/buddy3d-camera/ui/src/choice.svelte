<!--
  One labelled pick from a short list, as a row of the section's tables.
-->
<script lang="ts">
  import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
  } from '@prusa/connect-ui/primitives';

  import { held } from './held';

  let {
    label,
    value,
    items,
    disabled = false,
    onchange,
  }: {
    label: string;
    value: string;
    items: readonly { value: string; label: string }[];
    disabled?: boolean;
    onchange: (next: string) => void;
  } = $props();

  const { store } = held();
</script>

<dt class="self-center">{label}</dt>
<dd>
  <!-- Drawn again from the value and each refusal, so a choice the camera
       refused does not stay chosen. -->
  {#key `${value} ${store.problem}`}
    <Select {value} items={[...items]} {disabled} onValueChange={onchange}>
      <SelectTrigger size="sm" aria-label={label}><SelectValue /></SelectTrigger>
      <SelectContent>
        {#each items as option (option.value)}
          <SelectItem value={option.value} label={option.label}>{option.label}</SelectItem>
        {/each}
      </SelectContent>
    </Select>
  {/key}
</dd>
