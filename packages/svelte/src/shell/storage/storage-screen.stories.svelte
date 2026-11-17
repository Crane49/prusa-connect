<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { fakeListingStore } from '../../test/fakes';
  import StorageScreen from './storage-screen.svelte';
  import type {
    FolderSharing,
    StorageChoice,
    StorageRow,
    StorageSource,
  } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Storage',
    component: StorageScreen,
  });

  const store = fakeListingStore();

  const storageOf = (
    name: string,
    display_name: string,
    write = true,
  ): StorageChoice => ({
    allowed_extensions: ['.gcode', '.bgcode', '.3mf'],
    browse: true,
    display_name,
    download_url: false,
    // A storage that takes writes renames as well, which is what a move is.
    move: write,
    name,
    ranges: false,
    write,
  });

  const local = storageOf('local', 'Local disk');

  // Deliberately unsorted: folders first is the screen's own comparator, and
  // an already-sorted fixture would not prove it runs.
  const entries: StorageRow[] = [
    {
      folder: false,
      modified: '2026-08-18T14:02:00Z',
      name: 'Bracket_Left_Reinforced_v4_FINAL.gcode',
      path: 'Bracket_Left_Reinforced_v4_FINAL.gcode',
      size: 19_293_798,
    },
    {
      folder: true,
      modified: '2026-08-18T09:41:00Z',
      name: 'q3_rush_jobs',
      path: 'q3_rush_jobs',
      size: 0,
    },
    {
      folder: false,
      modified: '2026-08-17T11:12:00Z',
      name: 'bracket_v2.stl',
      path: 'bracket_v2.stl',
      size: 839_680,
    },
    {
      folder: true,
      modified: '2026-07-02T11:15:00Z',
      name: 'archive',
      path: 'archive',
      size: 0,
    },
    {
      folder: false,
      modified: '2026-08-16T20:11:00Z',
      name: 'benchy.3mf',
      path: 'benchy.3mf',
      size: 4_299_161,
    },
    {
      folder: false,
      modified: '2026-08-16T08:47:00Z',
      name: 'gasket_set_tpu.bgcode',
      path: 'gasket_set_tpu.bgcode',
      size: 2_411_724,
    },
  ];

  const nested: StorageRow[] = [
    {
      folder: false,
      modified: '2026-01-04T09:20:00Z',
      name: 'hinge_v1.gcode',
      path: 'archive/2025_prototypes/hinge_v1.gcode',
      size: 5_662_310,
    },
  ];

  const source = (over: Partial<StorageSource> = {}): StorageSource => ({
    cancelUpload: () => undefined,
    clearUploads: () => undefined,
    createFolder: () => Promise.resolve(),
    contentUrl: (name: string, path: string) =>
      `/api/v1/storages/${name}/content?path=${path}`,
    describe: () => Promise.resolve(),
    describedFile: () => undefined,
    directory: '',
    entries,
    error: undefined,
    filesError: undefined,
    filesLoaded: true,
    filesLoading: false,
    browseFolders: () => Promise.resolve(),
    load: () => Promise.resolve(),
    loaded: true,
    loading: false,
    moveAll: () => Promise.resolve([]),
    open: () => Promise.resolve(),
    pickerError: undefined,
    pickerFolders: entries.filter((row) => row.folder),
    pickerPath: '',
    remove: () => Promise.resolve(),
    storage: 'local',
    storages: [local],
    upload: () => Promise.resolve(undefined),
    uploads: [],
    ...over,
  });

  // `archive` is shared, so a folder inside it says so instead of a switch.
  const sharingOf = (canChange: boolean): FolderSharing => ({
    sharedRoot: (_storage: string, path: string) =>
      path === 'archive' || path.startsWith('archive/') ? 'archive' : undefined,
    canChange,
    share: () => Promise.resolve(),
  });

  const refused = {
    traceId: undefined,
    code: 'core.storage/INVALID_PATH',
    message: 'the path is outside the root',
    params: {},
    status: 400,
  };
</script>

<Story name="A Storage Of Files" asChild>
  <StorageScreen storage={source()} {store} />
</Story>

<Story name="Two Levels Down" asChild>
  <StorageScreen
    storage={source({
      directory: 'archive/2025_prototypes',
      entries: nested,
    })}
    {store}
  />
</Story>

<Story name="An Empty Folder" asChild>
  <StorageScreen
    storage={source({ directory: 'q3_rush_jobs', entries: [] })}
    {store}
  />
</Story>

<!-- A rename the server refuses is a toast, and the listing stays as it was. -->
<Story name="A Rename Refused" asChild>
  <StorageScreen
    storage={source({
      moveAll: (_name: string, moves: readonly { from: string }[]) =>
        Promise.resolve(
          moves.map((move) => ({ from: move.from, fault: refused })),
        ),
    })}
    {store}
  />
</Story>

<!-- A server in a Fractal network shares folders with the servers below. -->
<Story name="Shared Folders" asChild>
  <StorageScreen storage={source()} {store} sharing={sharingOf(true)} />
</Story>

<Story name="Inside A Shared Folder" asChild>
  <StorageScreen
    storage={source({
      directory: 'archive',
      entries: [
        {
          folder: true,
          modified: '2026-01-04T09:20:00Z',
          name: '2025_prototypes',
          path: 'archive/2025_prototypes',
          size: 0,
        },
      ],
    })}
    {store}
    sharing={sharingOf(true)}
  />
</Story>

<!-- A person who may not change the shares sees them as text. -->
<Story name="Shares Read Only" asChild>
  <StorageScreen storage={source()} {store} sharing={sharingOf(false)} />
</Story>

<Story name="Loading" asChild>
  <StorageScreen
    storage={source({ loaded: false, loading: true, storages: [] })}
    {store}
  />
</Story>

<Story name="Listing The Files" asChild>
  <StorageScreen
    storage={source({ entries: [], filesLoaded: false, filesLoading: true })}
    {store}
  />
</Story>

<Story name="The Server Refused" asChild>
  <StorageScreen
    storage={source({ entries: [], filesError: refused })}
    {store}
  />
</Story>

<!-- No storage is enabled at all, which is not an empty folder. -->
<Story name="Storage Not Enabled" asChild>
  <StorageScreen storage={source({ storage: '', storages: [] })} {store} />
</Story>

<Story name="More Than One Storage" asChild>
  <StorageScreen
    storage={source({
      storages: [local, storageOf('staging', 'Staging area', false)],
    })}
    {store}
  />
</Story>

<!-- A storage that takes no writes: no drop target and no delete. -->
<Story name="Read Only" asChild>
  <StorageScreen
    storage={source({ storages: [storageOf('local', 'Local disk', false)] })}
    {store}
  />
</Story>

<Story name="Uploads In Flight" asChild>
  <StorageScreen
    storage={source({
      uploads: [
        {
          fault: undefined,
          path: 'bracket_left_v5.gcode',
          sent: 7_549_747,
          state: 'sending',
          storage: 'local',
          total: 18_874_368,
        },
        {
          fault: undefined,
          path: 'clip_spare.gcode',
          sent: 0,
          state: 'cancelled',
          storage: 'local',
          total: 1_048_576,
        },
        {
          fault: undefined,
          path: 'gasket_set_tpu.bgcode',
          sent: 2_411_724,
          state: 'done',
          storage: 'local',
          total: 2_411_724,
        },
      ],
    })}
    {store}
  />
</Story>
