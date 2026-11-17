<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    fakeDevice,
    fakeFleetSource,
    fakeListingStore,
  } from '../../test/fakes';
  import TagsScreen from './tags-screen.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Tags',
    component: TagsScreen,
  });

  const TAGS: string[][] = [
    ['production', 'Brno'],
    ['production', 'Brno'],
    ['prototyping', 'Brno'],
    ['production', 'Prague', 'night shift'],
    ['maintenance', 'Prague'],
    ['PLA only', 'Prague'],
    ['PETG only', 'production'],
  ];

  const farm = () =>
    fakeFleetSource(
      TAGS.map((tags, index) => ({
        ...fakeDevice(`p${String(index)}`, `MK4S #${String(1000 + index)}`),
        tags,
      })),
    );

  const store = fakeListingStore();

  /** A store already holding the tiles, so the second view has a story. */
  const cardStore = {
    read: () => JSON.stringify({ view: 'cards' }),
    write: () => undefined,
  };

  const printersHref = (tag: string) => `#/printers?tag=${tag}`;
</script>

<!-- The tags a farm sorts its printers by, read off the printers themselves. -->

<Story name="The Tags A Farm Carries" asChild>
  <TagsScreen fleet={farm()} {store} {printersHref} />
</Story>

<Story name="Without A Way To The Printers List" asChild>
  <TagsScreen fleet={farm()} {store} />
</Story>

<Story name="The Same Tags As Tiles" asChild>
  <TagsScreen fleet={farm()} store={cardStore} {printersHref} />
</Story>

<Story name="Nothing Tagged Yet" asChild>
  <TagsScreen
    fleet={fakeFleetSource([fakeDevice('p0', 'MK4S #1000')])}
    {store}
  />
</Story>

<Story name="Still Arriving" asChild>
  <TagsScreen
    fleet={fakeFleetSource([], { loading: true, loaded: false })}
    {store}
  />
</Story>

<Story name="The Fleet Could Not Be Read" asChild>
  <TagsScreen
    fleet={fakeFleetSource([], {
      error: new Error('the fleet could not be read'),
    })}
    {store}
  />
</Story>
