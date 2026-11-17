<script module lang="ts">
  import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
  import PencilLineIcon from '@lucide/svelte/icons/pencil-line';
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import TitleButton from './title-button.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/TitleButton',
    component: TitleButton,
    args: {
      level: 'page',
      icon: ChevronsUpDownIcon,
      label: 'Workshop, switch dashboard',
      title: 'Workshop',
      children: name,
    },
  });
</script>

{#snippet name()}Workshop{/snippet}
{#snippet long()}A dashboard with a very long name that cannot fit the row{/snippet}

<Story name="Page" />

<Story
  name="Section"
  args={{
    level: 'section',
    icon: PencilLineIcon,
    label: 'Rename Workshop',
  }}
/>

<Story name="Truncated Name" asChild>
  <div class="w-64">
    <h1 class="min-w-0">
      <TitleButton
        icon={ChevronsUpDownIcon}
        label="A dashboard with a very long name that cannot fit the row, switch"
        title="A dashboard with a very long name that cannot fit the row"
      >
        {@render long()}
      </TitleButton>
    </h1>
  </div>
</Story>

<Story name="Disabled" args={{ disabled: true }} />
