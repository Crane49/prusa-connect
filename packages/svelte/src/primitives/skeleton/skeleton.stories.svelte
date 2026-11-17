<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import CardContent from '../card/card-content.svelte';
  import Card from '../card/card.svelte';
  import Skeleton from './skeleton.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Skeleton',
    component: Skeleton,
  });
</script>

<Story name="Default" args={{ class: 'h-4 w-48' }} />

<Story name="On Both Surfaces" asChild>
  <div class="flex gap-6">
    <div class="flex flex-col gap-2">
      <Skeleton class="h-4 w-40" />
      <Skeleton class="h-4 w-32" />
      <Skeleton class="size-10" />
    </div>
    <Card class="w-64">
      <CardContent class="flex flex-col gap-2">
        <Skeleton class="h-4 w-40" />
        <Skeleton class="h-4 w-32" />
        <Skeleton class="size-10" />
      </CardContent>
    </Card>
  </div>
</Story>
