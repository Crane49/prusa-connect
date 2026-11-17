<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import FieldPicker from './field-picker.svelte';
  import type { ListField, VisibleFields } from './types';

  const { Story } = defineMeta({
    title: 'Primitives/FieldPicker',
    component: FieldPicker,
  });

  const columns: ListField[] = [
    { id: 'file', label: 'File', required: true },
    { id: 'printer', label: 'Printer' },
    { id: 'state', label: 'State' },
    { id: 'progress', label: 'Progress' },
    { id: 'started', label: 'Started', hiddenByDefault: true },
  ];

  const identity: ListField[] = [
    { id: 'file', label: 'File', required: true },
    { id: 'actions', label: 'Actions', required: true },
  ];

  const defaultsOf = (fields: ListField[]): string[] =>
    fields
      .filter((field) => field.required === true || !field.hiddenByDefault)
      .map((field) => field.id);
</script>

<script lang="ts">
  // A picker that does not move when it is ticked shows half the component, so
  // each story holds the set its own listing draws.
  const held = $state({
    plain: defaultsOf(columns),
    away: ['file', 'state'],
    fixed: defaultsOf(identity),
  });

  const hold = (of: 'plain' | 'away' | 'fixed', on: ListField[]) =>
    ({
      get visible() {
        return held[of];
      },
      onToggle: (id: string) => {
        held[of] = held[of].includes(id)
          ? held[of].filter((kept) => kept !== id)
          : [...held[of], id];
      },
      onReset: () => {
        held[of] = defaultsOf(on);
      },
    }) satisfies VisibleFields;
</script>

<!--
  Beyond the React set. React ships no field-picker.stories.tsx, so nothing
  here is a port. These are the states the props allow: a listing at its
  defaults, one a reader has changed, and one whose fields are all identity.
-->

<Story name="Default" asChild>
  <FieldPicker fields={columns} state={hold('plain', columns)} />
</Story>

<Story name="Fields Put Away" asChild>
  <FieldPicker fields={columns} state={hold('away', columns)} />
</Story>

<Story name="Nothing That May Be Hidden" asChild>
  <FieldPicker fields={identity} state={hold('fixed', identity)} />
</Story>
