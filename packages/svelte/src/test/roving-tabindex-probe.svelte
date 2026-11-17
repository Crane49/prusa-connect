<!--
  A radio group described from outside itself, for the test that shows why a
  `Field` refuses to wrap one: Tab lands on the checked item, never on the
  root, so the description written on the root is never read out.
-->
<script lang="ts">
  import { RadioGroup } from '../primitives/radio-group';
  import RadioGroupItem from '../primitives/radio-group/radio-group-item.svelte';
</script>

<span id="roving-hint">Firmware default is 0.4mm.</span>
<RadioGroup aria-describedby="roving-hint" value="a">
  <RadioGroupItem value="a" id="roving-a" aria-label="A" />
  <RadioGroupItem value="b" id="roving-b" aria-label="B" />
</RadioGroup>
