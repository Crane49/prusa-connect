<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import NoLongerHere from './no-longer-here.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/NoLongerHere',
    component: NoLongerHere,
    args: {
      heading: 'This printer is not here',
      headingLevel: 'h1',
      description:
        'It was removed, or you may no longer see it, or the link is wrong.',
      linkLabel: 'Printers',
      href: '/printers',
    },
  });
</script>

<Story name="Default" />
