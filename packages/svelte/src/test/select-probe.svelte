<!-- The printer model select the select tests drive. -->
<script lang="ts">
  import type { ControlSize } from '../primitives/control-trigger/types';
  import {
    Select,
    SelectContent,
    SelectGroup,
    SelectItem,
    SelectLabel,
    SelectSeparator,
    SelectTrigger,
    SelectValue,
  } from '../primitives/select';

  let {
    size = 'md',
    disabled = false,
    value = $bindable('mk4s'),
  }: {
    size?: ControlSize;
    disabled?: boolean;
    value?: string;
  } = $props();

  const items = [
    { value: 'mk4s', label: 'MK4S' },
    { value: 'mini', label: 'MINI+' },
    { value: 'xl', label: 'XL', disabled: true },
    { value: 'sl1s', label: 'SL1S SPEED' },
  ];
</script>

<Select bind:value {disabled} {items}>
  <SelectTrigger {size} aria-label="Printer model" class="w-48">
    <SelectValue placeholder="Select a printer" />
  </SelectTrigger>
  <SelectContent>
    <SelectGroup>
      <SelectLabel>FDM</SelectLabel>
      <SelectItem value="mk4s" label="MK4S">MK4S</SelectItem>
      <SelectItem value="mini" label="MINI+">MINI+</SelectItem>
      <SelectItem value="xl" label="XL" disabled>XL</SelectItem>
    </SelectGroup>
    <SelectSeparator />
    <SelectGroup>
      <SelectLabel>Resin</SelectLabel>
      <SelectItem value="sl1s" label="SL1S SPEED">SL1S SPEED</SelectItem>
    </SelectGroup>
  </SelectContent>
</Select>

<!-- The bound value, so a test can read what the binding carried back. -->
<p data-testid="chosen">{value}</p>
