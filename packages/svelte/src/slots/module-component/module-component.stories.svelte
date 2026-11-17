<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import type { Component } from 'svelte';

  import PickerComponent from '../../test/picker-component.svelte';
  import { ComponentUnavailable } from '../host';
  import ModuleComponent from './module-component.svelte';

  // Another module's component inside a module's page: loading, drawn, and
  // not available with and without the importing module's own fallback
  // (ADR-20261027-wvj rule 24).

  const { Story } = defineMeta({
    title: 'Parts/Module Component',
    component: ModuleComponent,
  });

  interface PickerProps extends Record<string, unknown> {
    items: readonly string[];
    onselect: (item: string) => void;
  }

  const picker = (): Promise<Component<PickerProps>> =>
    Promise.resolve(PickerComponent);
  const never = (): Promise<Component<PickerProps>> =>
    new Promise<Component<PickerProps>>(() => undefined);
  const absent = (): Promise<Component<PickerProps>> =>
    Promise.reject(
      new ComponentUnavailable('absent', 'spools', 'spool-picker'),
    );

  const props: PickerProps = {
    items: ['PLA Galaxy Black', 'PETG Orange'],
    onselect: () => undefined,
  };
</script>

<Story name="Drawn" asChild>
  <ModuleComponent load={picker} {props} />
</Story>

<Story name="Loading" asChild>
  <ModuleComponent load={never} {props} />
</Story>

<Story name="Not Available" asChild>
  <ModuleComponent load={absent} {props} />
</Story>

<Story name="Not Available With A Fallback" asChild>
  <ModuleComponent load={absent} {props}>
    {#snippet fallback(why: ComponentUnavailable)}
      <p class="text-sm">Pick a spool by hand ({why.reason}).</p>
    {/snippet}
  </ModuleComponent>
</Story>
