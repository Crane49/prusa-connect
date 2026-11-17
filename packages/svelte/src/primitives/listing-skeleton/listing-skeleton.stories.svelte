<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ListingSkeleton from './listing-skeleton.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Listing Skeleton',
    component: ListingSkeleton,
    args: { label: 'Loading jobs' },
  });
</script>

<Story name="A Listing On Its Way" />

<!-- For a listing whose rows start with a picture. -->
<Story name="With Row Art" args={{ label: 'Loading the spools', art: true }} />

<!-- It says what is loading. A pulse is invisible to a screen reader, so the
     name is the whole of what that reader gets. -->
<Story name="It Announces What Is Coming" tags={['!dev', '!autodocs']} />

<!-- Beyond the React set -->

<Story name="One Row" args={{ rows: 1 }} />
