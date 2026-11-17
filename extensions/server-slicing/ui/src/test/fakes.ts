// What the stories and the tests draw instead of the server: the module's
// English texts, and answers in the shape the routes give.
import {
  type FaultText,
  locale,
  registerCatalogue,
  translateFor,
} from '@prusa/connect-ui/i18n';

import type {
  InstalledSlicer,
  Job,
  JobList,
  Offer,
  Slicers,
  Translate,
} from '../api';
import english from '../locales/en.json';
import type { Load, SlicerView } from '../store.svelte';

// The module's English texts, read the way the host reads them, so a plural
// message is drawn as a person sees it.
const NAMESPACE = 'com.github.aux.prusa3d.server-slicing';
registerCatalogue(NAMESPACE, 'en', english);
locale.set('en');

/** The module's English texts, with the params filled in. */
export const t: Translate = translateFor(NAMESPACE);

const catalogue = {
  github: true,
  fetched_at: '2026-10-27T06:00:00Z',
  supported: '>=2.7.0 <4.0.0',
};

export const noSlicers: Slicers = {
  platform: 'linux/arm64',
  installed: [],
  available: [],
  installing: [],
  catalogue: { github: false, supported: '>=2.7.0 <4.0.0' },
  problems: [],
};

const notes = (version: string) =>
  `https://github.com/prusa3d/PrusaSlicer/releases/tag/version_${version}`;

const github296: InstalledSlicer = {
  version: '2.9.6',
  channel: 'stable',
  source: 'github',
  sha256: '94fd7b8a9f87c9631e1c71739b15b184fc5f4c0ceabd69072f1c78f229a4fe40',
  verified: true,
  installed_at: '2026-10-26T22:00:00Z',
  released: '2026-06-25',
  notes: notes('2.9.6'),
  bytes: 362_811_392,
};

const local295: InstalledSlicer = {
  version: '2.9.5',
  channel: 'stable',
  source: 'local',
  bytes: 41_200_000,
};

export const alphaOffer: Offer = {
  version: '3.0.0-alpha12',
  channel: 'prerelease',
  source: 'github',
  size: 135943272,
  released: '2026-09-21',
  notes: notes('3.0.0-alpha12'),
  asset: 'PrusaSlicer-3.0.0-alpha12.dmg',
  sha256: 'a1f79dda1745e070774999f056d839bb5bd4a971d7f4cd876de3dde93ec5a436',
  verified: true,
  compatible: true,
  installed: false,
};

export const stableOffer: Offer = {
  version: '2.9.6',
  channel: 'stable',
  source: 'github',
  size: 142444072,
  released: '2026-06-25',
  notes: notes('2.9.6'),
  asset: 'PrusaSlicer-2.9.6.dmg',
  sha256: '94fd7b8a9f87c9631e1c71739b15b184fc5f4c0ceabd69072f1c78f229a4fe40',
  verified: true,
  compatible: true,
  installed: true,
};

/** A release from before GitHub published digests. */
export const uncheckedOffer: Offer = {
  version: '2.9.2',
  channel: 'stable',
  source: 'github',
  size: 136581346,
  released: '2025-04-10',
  notes: notes('2.9.2'),
  asset: 'PrusaSlicer-2.9.2.dmg',
  verified: false,
  compatible: true,
  installed: false,
};

const newerOffer: Offer = {
  version: '2.9.7',
  channel: 'stable',
  source: 'github',
  size: 142900000,
  released: '2026-10-20',
  notes: notes('2.9.7'),
  verified: true,
  compatible: true,
  installed: false,
};

const rcOffer: Offer = {
  version: '2.9.6-rc1',
  channel: 'prerelease',
  source: 'github',
  size: 152490689,
  released: '2026-06-17',
  notes: notes('2.9.6-rc1'),
  verified: true,
  compatible: true,
  installed: false,
};

/** A release in the range with no build for the host. */
export const missingOffer: Offer = {
  version: '2.9.6',
  channel: 'stable',
  source: 'github',
  released: '2026-06-25',
  notes: notes('2.9.6'),
  verified: false,
  compatible: false,
  reason: 'no build for linux/arm64',
  installed: false,
};

export const someSlicers: Slicers = {
  platform: 'darwin/arm64',
  installed: [github296, local295],
  available: [alphaOffer, stableOffer, rcOffer, uncheckedOffer],
  installing: [],
  catalogue,
  default: '2.9.6',
  problems: [],
};

/** What an administrator reads: the same, with each executable's path. */
export const adminSlicers: Slicers = {
  ...someSlicers,
  installed: [
    {
      ...github296,
      path: '/var/lib/prusa-connect/modules/slicer/data/slicers/2.9.6-8124/Original Prusa Drivers/PrusaSlicer.app/Contents/MacOS/PrusaSlicer',
    },
    { ...local295, path: '/opt/prusa-slicer-cli/bin/prusa-slicer' },
  ],
};

export const installingSlicers: Slicers = {
  ...adminSlicers,
  installing: [
    {
      version: '3.0.0-alpha12',
      phase: 'downloading',
      done_bytes: 61234567,
      total_bytes: 135943272,
    },
  ],
};

export const unpackingSlicers: Slicers = {
  ...adminSlicers,
  installing: [
    {
      version: '3.0.0-alpha12',
      phase: 'unpacking',
      done_bytes: 135943272,
      total_bytes: 135943272,
    },
  ],
};

export const troubledSlicers: Slicers = {
  ...adminSlicers,
  installing: [
    {
      version: '3.0.0-alpha12',
      phase: 'failed',
      done_bytes: 0,
      error:
        'the archive cannot be unpacked: hdiutil attach: exit status 1: hdiutil: attach failed - Device not configured',
    },
  ],
  problems: [
    {
      what: '/opt/prusa-slicer/bin/prusa-slicer',
      error: 'the executable did not name a PrusaSlicer version',
    },
  ],
  pinned: '2.9.5',
  default: '2.9.5',
};

/** A newer final release than the default. */
export const updateSlicers: Slicers = {
  ...adminSlicers,
  available: [newerOffer, ...someSlicers.available],
  update: newerOffer,
};

/** GitHub could not be read, and the list read before is shown. */
export const offlineSlicers: Slicers = {
  ...adminSlicers,
  catalogue: {
    ...catalogue,
    error:
      'read the release list from GitHub: dial tcp: lookup api.github.com: no such host',
  },
};

/** A Raspberry Pi: GitHub has builds of 2.7 for it and none of the newer ones. */
export const linuxSlicers: Slicers = {
  platform: 'linux/arm64',
  installed: [],
  available: [
    {
      version: '3.0.0-alpha12',
      channel: 'prerelease',
      source: 'github',
      released: '2026-09-21',
      notes: notes('3.0.0-alpha12'),
      verified: false,
      compatible: false,
      reason: 'no build for linux/arm64',
      installed: false,
    },
    missingOffer,
    {
      version: '2.7.4',
      channel: 'stable',
      source: 'github',
      size: 82116066,
      released: '2024-04-05',
      notes: notes('2.7.4'),
      verified: false,
      compatible: true,
      installed: false,
    },
  ],
  installing: [],
  catalogue,
  problems: [],
};

const base = {
  by: 'user:alice',
  source: { storage: 'local', path: 'plates/brackets.3mf' },
  output: { storage: 'local', path: 'plates/brackets' },
  selection: { bed: 0 },
  overwrite: false,
  created_at: '2026-10-26T21:40:00Z',
  progress: 0,
} satisfies Partial<Job>;

export const someJobs: JobList = {
  all: false,
  total: 4,
  jobs: [
    {
      ...base,
      id: 'a1',
      state: 'running',
      progress: 45,
      step: 'Making infill',
      slicer: '2.9.6',
      started_at: '2026-10-26T21:40:02Z',
      selection: { bed: 1, items: [3, 4] },
    },
    { ...base, id: 'a2', state: 'queued' },
    {
      ...base,
      id: 'a3',
      state: 'done',
      progress: 100,
      started_at: '2026-10-26T21:40:01Z',
      finished_at: '2026-10-26T21:40:39Z',
      result: {
        storage: 'local',
        path: 'plates/brackets.bgcode',
        size: 50623,
        format: 'bgcode',
        slicer: '2.9.6',
        duration_ms: 1460,
        estimated_seconds: 4269,
        objects: [
          { id: 0, name: 'Bracket (Instance 1)', item: 0 },
          { id: 1, name: 'Bracket (Instance 2)', item: 2 },
        ],
        thumbnails: [{ format: 'png', width: 220, height: 124 }],
      },
    },
    {
      ...base,
      id: 'a4',
      state: 'failed',
      source: { upload: 'gear.3mf' },
      started_at: '2026-10-26T21:40:00Z',
      finished_at: '2026-10-26T21:40:01Z',
      error: {
        code: 'server-slicing/NOTHING_TO_PRINT',
        message: 'All objects are outside of the print volume.',
      },
    },
  ],
};

export const everybodysJobs: JobList = {
  ...someJobs,
  all: true,
  jobs: someJobs.jobs.map((job, index) => ({
    ...job,
    by: index % 2 ? 'user:bob' : 'user:alice',
  })),
};

export const noJobs: JobList = { all: false, total: 0, jobs: [] };

const failed: FaultText = {
  code: undefined,
  title: 'The server did not answer',
  detail: 'Try again in a moment.',
};

/** A view in one state, with actions that do nothing. */
export const viewOf = (
  load: Load,
  slicers?: Slicers,
  jobs?: JobList,
): SlicerView => ({
  load,
  fault: load === 'failed' || load === 'refused' ? failed : undefined,
  actionFault: undefined,
  slicers,
  jobs,
  install: () => Promise.resolve(),
  checkReleases: () => Promise.resolve(),
  remove: () => Promise.resolve(),
  pin: () => Promise.resolve(),
  cancel: () => Promise.resolve(),
  submit: () => Promise.resolve('j1'),
});

/** Every release GitHub lists from 2.7.0, enough for a second page. */
export const manySlicers: Slicers = (() => {
  const available: Offer[] = [];
  for (const [minor, patches] of [
    [7, 4],
    [8, 1],
    [9, 6],
  ] as const) {
    for (let patch = patches; patch >= 0; patch -= 1) {
      const version = `2.${String(minor)}.${String(patch)}`;
      for (const label of ['', '-rc1', '-beta1', '-alpha1']) {
        available.push({
          version: `${version}${label}`,
          channel: label === '' ? 'stable' : 'prerelease',
          source: 'github',
          size: 130_000_000 + patch * 1_000_000,
          released: `202${String(minor - 3)}-0${String(patch + 1)}-1${label === '' ? '5' : '0'}`,
          notes: notes(`${version}${label}`),
          asset: `PrusaSlicer-${version}${label}.dmg`,
          verified: minor === 9 && patch >= 3,
          compatible: true,
          installed: false,
        });
      }
    }
  }

  return { ...adminSlicers, available: [alphaOffer, ...available] };
})();

/** An operator's build with a name that does not fit a column. */
export const longSlicers: Slicers = {
  ...adminSlicers,
  installed: [
    ...adminSlicers.installed,
    {
      version: '2.9.4-alpha1234567890-with-a-very-long-build-label',
      channel: 'prerelease',
      source: 'operator',
      sha256: 'b'.repeat(64),
      verified: true,
      installed_at: '2026-10-20T10:00:00Z',
      bytes: 401_000_000,
      path: '/srv/prusa-connect/a/very/deep/folder/that/an/operator/chose/for/their/own/builds/of/prusaslicer/2.9.4-alpha1234567890/bin/prusa-slicer',
    },
  ],
};

/** A job with a long project name and the end of what the slicer printed. */
export const longJobs: JobList = {
  ...everybodysJobs,
  jobs: [
    {
      ...base,
      id: 'b1',
      state: 'failed',
      by: 'user:a-person-with-a-very-long-account-name@example.org',
      source: {
        storage: 'local',
        path: 'customers/2026/october/a-project-with-a-name-far-too-long-for-any-column-of-the-table-v12-final-final.3mf',
      },
      slicer: '2.9.6',
      started_at: '2026-10-26T21:40:00Z',
      finished_at: '2026-10-26T21:41:13Z',
      error: {
        code: 'server-slicing/SLICER_FAILED',
        message: 'PrusaSlicer exited with status 1',
        log: [
          'Loading project customers/2026/october/a-project.3mf',
          'Objects: 12, instances: 24',
          'Processing triangulated mesh',
          'Generating perimeters: 100%',
          'error: Some objects are too tall and cannot be printed without extruder collisions.',
        ],
      },
    },
    ...everybodysJobs.jobs,
  ],
};

/** Enough jobs for a second page, over three slicers and five projects. */
export const manyJobs: JobList = {
  all: true,
  total: 64,
  jobs: Array.from({ length: 64 }, (_, index): Job => {
    const states = ['done', 'done', 'failed', 'canceled'] as const;
    const minute = String(index % 60).padStart(2, '0');

    return {
      ...base,
      id: `m${String(index)}`,
      state: index === 0 ? 'running' : states[index % states.length]!,
      progress: index === 0 ? 30 : 100,
      by: index % 3 ? 'user:bob' : 'user:alice',
      source: {
        storage: 'local',
        path: `plates/part-${String(index % 5)}.3mf`,
      },
      slicer: ['2.9.6', '2.9.5', '3.0.0-alpha12'][index % 3] ?? '2.9.6',
      created_at: `2026-10-2${String(index % 7)}T1${String(index % 10)}:${minute}:00Z`,
      started_at: `2026-10-2${String(index % 7)}T1${String(index % 10)}:${minute}:01Z`,
      ...(index === 0
        ? {}
        : {
            finished_at: `2026-10-2${String(index % 7)}T1${String(index % 10)}:${minute}:4${String(index % 10)}Z`,
          }),
    };
  }),
};
