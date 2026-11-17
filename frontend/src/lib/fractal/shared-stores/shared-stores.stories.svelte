<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import { ApiError } from '@prusa/connect';

  import {
    FractalStores,
    type FractalStoresClient,
    type StoreEntry,
  } from '../../stores/fractal-stores.svelte';
  import SharedStores from './shared-stores.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Fractal/Shared By Other Servers',
    component: SharedStores,
  });

  type Status = 'OK' | 'STALE';

  const storeOf = (
    node: string,
    instance: string,
    status: Status = 'OK',
    rootHash: string | null = `root-${node}-${instance}`,
  ) => ({
    node,
    interface: 'ContentStore1',
    instance,
    status,
    updated_at: 1_793_700_000_000,
    props: { rootHash },
  });

  const folder = (name: string): StoreEntry => ({
    name,
    kind: 'directory',
    size: 0,
    identity: `dir-${name}`,
  });

  const file = (name: string, size: number): StoreEntry => ({
    name,
    kind: 'file',
    size,
    identity: `file-${name}`,
  });

  const listings: Record<string, StoreEntry[]> = {
    'root-hq-local': [folder('projects'), file('benchy.bgcode', 2_411_724)],
    'root-hq-gdrive': [folder('fixtures')],
    'root-hq/brno/zabovresky-local': [folder('jigs')],
  };

  const shared = (
    items: ReturnType<typeof storeOf>[],
    answer: (identity: string) => Promise<StoreEntry[]> = (identity) =>
      Promise.resolve(listings[identity] ?? []),
  ): FractalStores => {
    const client: FractalStoresClient = {
      listFractalStores: () =>
        Promise.resolve({
          node: 'hq/brno',
          items,
          total: items.length,
          truncated: false,
        }),
      readFractalStoreListing: async ({ path }) => ({
        ...path,
        entries: await answer(path.identity),
      }),
    };

    return new FractalStores(client);
  };

  const refusal = new ApiError(403, {
    type: 'about:blank',
    status: 403,
    code: 'core.authz/FORBIDDEN',
    title: 'refused',
    detail: 'this person may not read the link',
    params: {},
    retryable: false,
  });

  const never = (): FractalStores =>
    new FractalStores({
      listFractalStores: () => new Promise(() => undefined),
      readFractalStoreListing: () => new Promise(() => undefined),
    });

  const refused = (): FractalStores =>
    new FractalStores({
      listFractalStores: () => Promise.reject(refusal),
      readFractalStoreListing: () => Promise.reject(refusal),
    });
</script>

<!--
  Omitted Folder: a folder opens on a click, and its rows are drawn the same.
-->

<Story name="Two Servers Share Folders" asChild>
  <SharedStores
    stores={shared([
      storeOf('hq', 'local'),
      storeOf('hq', 'gdrive'),
      storeOf('hq/brno/zabovresky', 'local'),
    ])}
  />
</Story>

<Story name="A Server Not Heard From" asChild>
  <SharedStores
    stores={shared([
      storeOf('hq', 'local'),
      storeOf('hq/brno/zabovresky', 'local', 'STALE'),
    ])}
  />
</Story>

<Story name="A Storage That Did Not Answer" asChild>
  <SharedStores
    stores={shared(
      [storeOf('hq', 'local'), storeOf('hq/brno/zabovresky', 'local')],
      (identity) =>
        identity === 'root-hq-local'
          ? Promise.reject(refusal)
          : Promise.resolve(listings[identity] ?? []),
    )}
  />
</Story>

<!-- The owner has not finished its first scan, which is not an empty store. -->
<Story name="A Storage Not Listed Yet" asChild>
  <SharedStores stores={shared([storeOf('hq', 'usb', 'OK', null)])} />
</Story>

<Story name="Empty" asChild>
  <SharedStores stores={shared([])} />
</Story>

<Story name="Loading" asChild>
  <SharedStores stores={never()} />
</Story>

<Story name="Refused" asChild>
  <SharedStores stores={refused()} />
</Story>
