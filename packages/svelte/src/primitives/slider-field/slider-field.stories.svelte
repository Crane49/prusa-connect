<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SliderFieldHarness from '../../test/slider-field-harness.svelte';
  import SliderField from './slider-field.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Slider Field',
    component: SliderField,
    args: {
      value: 100,
      min: 50,
      max: 200,
      unit: '%',
      label: 'Speed override',
      onCommit: () => undefined,
    },
  });
</script>

<Story name="Default" asChild>
  <SliderFieldHarness />
</Story>

<Story name="Typing Commits On Enter" tags={['!dev', '!autodocs']} asChild>
  <SliderFieldHarness />
</Story>

<Story name="Out Of Range Clamps" tags={['!dev', '!autodocs']} asChild>
  <SliderFieldHarness />
</Story>

<Story name="Empty Field Keeps The Value" tags={['!dev', '!autodocs']} asChild>
  <SliderFieldHarness initial={120} />
</Story>

<Story name="Disabled" args={{ disabled: true, value: 95 }} />
