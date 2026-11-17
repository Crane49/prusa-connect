<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Spinner from './spinner.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Spinner',
    component: Spinner,
  });
</script>

<Story name="Default" />

<Story name="Medium" args={{ size: 'md' }} />

<Story name="Large" args={{ size: 'lg' }} />

<Story name="Primary" args={{ tone: 'primary' }} />

<Story name="Muted" args={{ tone: 'muted' }} />

<Story name="Hidden From Assistive Tech" tags={['!dev', '!autodocs']} />
