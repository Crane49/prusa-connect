// The plans the stories and the tests of this component draw.
import { ApiError } from '@prusa/connect';

import type {
  InstallPlan,
  ModuleFileTypeConflict,
} from '../../stores/module-catalogue.svelte';

const reader = {
  module: 'github.com/acme/reader',
  name: 'reader',
  title: 'Reader',
  version: '1.4.0',
  source: 'https://example.test/reader-1.4.0.tar.gz',
  namespace: 'com.github.aux.acme.reader',
  origin: 'marketplace' as const,
  installed: false,
  process: true,
  requests_known: true,
  permissions: ['read_storage'],
  required_by: ['com.github.aux.acme.slicer ^1.2'],
};

export const loading: InstallPlan = { state: 'loading' };

/** An install that adds two readers, each with a recommended memory budget. */
export const addsTwo: InstallPlan = {
  state: 'ready',
  dependencies: [
    { ...reader, memory_mib: 128 },
    {
      ...reader,
      module: 'github.com/acme/viewer',
      name: 'viewer',
      title: 'Viewer',
      namespace: 'com.github.aux.acme.viewer',
      memory_mib: 256,
    },
  ],
  conflicts: [],
};

/** An install that adds a UI-only panel, which has no memory budget, then a reader. */
export const addsUIOnly: InstallPlan = {
  state: 'ready',
  dependencies: [
    {
      ...reader,
      module: 'github.com/acme/panel',
      name: 'panel',
      title: 'Panel',
      namespace: 'com.github.aux.acme.panel',
      process: false,
    },
    { ...reader, memory_mib: 256 },
  ],
  conflicts: [],
};

/** A slicer whose install adds a reader. */
export const adds: InstallPlan = {
  state: 'ready',
  dependencies: [reader],
  conflicts: [],
};

/** A reader of .3mf files while another one is switched on. */
const fileTypes: readonly ModuleFileTypeConflict[] = [
  {
    module: 'com.github.aux.acme.reader',
    with: 'com.github.aux.other.projects',
    use: 'file_analysis',
    types: ['.3mf'],
  },
  {
    module: 'com.github.aux.acme.reader',
    with: 'com.github.aux.other.projects',
    use: 'file_view',
    types: ['.3mf'],
  },
];

export const fileTypeConflict: InstallPlan = {
  state: 'ready',
  dependencies: [],
  conflicts: fileTypes,
};

/** Two ranges no release of the reader meets. */
export const conflict: InstallPlan = {
  state: 'refused',
  error: new ApiError(409, {
    type: 'https://prusa-connect/errors/core.resource/MODULE_DEPENDENCY_CONFLICT',
    status: 409,
    retryable: false,
    code: 'core.resource/MODULE_DEPENDENCY_CONFLICT',
    title: 'refused',
    detail: 'no release fits',
    params: {
      resource: 'com.github.aux.acme.slicer',
      requires:
        'com.github.aux.acme.slicer needs com.github.aux.acme.reader ^2.0, com.github.aux.acme.planner needs com.github.aux.acme.reader ^1.0',
    },
  }),
};

/** A reader no list this server reads offers. */
export const notOffered: InstallPlan = {
  state: 'refused',
  error: new ApiError(409, {
    type: 'https://prusa-connect/errors/core.resource/MODULE_DEPENDENCY_NOT_OFFERED',
    status: 409,
    retryable: false,
    code: 'core.resource/MODULE_DEPENDENCY_NOT_OFFERED',
    title: 'refused',
    detail: 'not offered',
    params: {
      resource: 'com.github.aux.acme.slicer',
      requires:
        'com.github.aux.acme.slicer needs com.github.aux.acme.reader ^1.2',
    },
  }),
};

/**
 * An install that adds eight extensions, each asking for a lot, and meets two
 * file type conflicts: taller than any window, so the dialog body scrolls.
 */
export const longPlan: InstallPlan = {
  state: 'ready',
  dependencies: Array.from({ length: 8 }, (_, index) => ({
    ...reader,
    module: `github.com/acme/helper-${String(index + 1)}`,
    name: `helper-${String(index + 1)}`,
    title: `Helper ${String(index + 1)}`,
    namespace: `com.github.aux.acme.helper-${String(index + 1)}`,
    permissions: [
      'read_storage',
      'write_storage',
      'observe',
      'operate',
      'printers.read',
      'jobs.read',
    ],
    events: { 'helper.ready': 'ready' },
    records_mib: 32,
  })),
  conflicts: fileTypes,
};
