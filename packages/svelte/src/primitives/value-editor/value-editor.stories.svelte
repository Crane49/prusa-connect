<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { NOZZLE_PRESETS } from '../../test/nozzle-presets';
  import ValueEditor from './value-editor.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/ValueEditor',
    component: ValueEditor,
    args: {
      label: 'Nozzle',
      subject: 'Tool 1',
      currentLabel: '234.6°C',
      target: 235,
      unit: '°C',
      min: 0,
      max: 300,
      step: 5,
      presets: NOZZLE_PRESETS,
      onCommit: () => undefined,
    },
  });
</script>

<Story name="Default" />

<Story name="Unavailable" args={{ disabled: true }} />

<Story name="Open" />

<Story
  name="Choices"
  args={{
    subject: undefined,
    choices: [
      { key: '1', subject: 'Tool 1', currentLabel: '185°C', target: 0 },
      {
        key: '2',
        subject: 'Tool 2',
        currentLabel: '240°C',
        target: 240,
        targetLabel: 'Target for T2 (°C)',
      },
    ],
  }}
/>

<Story name="Nothing Is Committed Until Set" tags={['!dev', '!autodocs']} />

<Story name="Closing Discards The Draft" tags={['!dev', '!autodocs']} />

<Story name="An Out Of Range Figure Is Clamped" tags={['!dev', '!autodocs']} />

<Story
  name="Two Presets Sharing A Figure Light Only The One Pressed"
  tags={['!dev', '!autodocs']}
/>

<Story
  name="Typing A Figure Clears The Preset Highlight"
  tags={['!dev', '!autodocs']}
/>

<Story name="Every Preset Is One Tap Away" />
