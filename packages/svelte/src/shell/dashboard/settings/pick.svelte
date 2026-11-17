<!--
  One setting picked from a list: a printer, a camera, a tag. The library's
  select gives the keyboard walk and the typeahead.

  An empty value is a real choice here, "every printer" or "none yet", and
  the select cannot hold an empty string as an option, so it is spelled as
  NONE inside this component and as an empty string everywhere else.
-->
<script lang="ts">
  import {
    Label,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
  } from '../../../primitives';

  let {
    label,
    value,
    options,
    empty,
    note,
    onchange,
  }: {
    label: string;
    value: string;
    options: readonly { value: string; label: string }[];
    /** What an empty value reads as. */
    empty: string;
    note?: string | undefined;
    onchange: (next: string) => void;
  } = $props();

  const NONE = '\u0000none';

  const id = $props.id();
  const noteId = `${id}-note`;

  const items = $derived([{ value: NONE, label: empty }, ...options]);
  // A value the list no longer holds, such as a printer that was removed, is
  // kept and shown by its own spelling, so opening the settings changes
  // nothing until a person picks.
  const shown = $derived(
    value === '' || items.some((item) => item.value === value)
      ? items
      : [...items, { value, label: value }],
  );

  const picked = (next: string): void => {
    onchange(next === NONE ? '' : next);
  };
</script>

<div class="grid gap-2">
  <Label for={id}>{label}</Label>
  <Select
    value={value === '' ? NONE : value}
    items={shown}
    onValueChange={picked}
  >
    <SelectTrigger
      {id}
      aria-describedby={note === undefined ? undefined : noteId}
    >
      <SelectValue />
    </SelectTrigger>
    <SelectContent>
      {#each shown as item (item.value)}
        <SelectItem value={item.value} label={item.label}
          >{item.label}</SelectItem
        >
      {/each}
    </SelectContent>
  </Select>
  {#if note !== undefined}
    <p id={noteId} class="text-xs text-muted-foreground">{note}</p>
  {/if}
</div>
