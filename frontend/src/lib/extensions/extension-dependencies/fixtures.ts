// The dependencies the stories and the tests of this component draw.
import type { ExtensionDependencies } from '../dependencies';
import type {
  OutdatedCheck,
  WhyCheck,
} from '../../stores/module-catalogue.svelte';

export const READER = 'com.github.aux.acme.reader';
export const PARTS = 'com.github.aux.acme.parts';
export const SLICER = 'com.github.aux.acme.slicer';

const none: ExtensionDependencies = {
  core: '',
  requires: [],
  suggests: [],
  conflicts: [],
  requiredBy: [],
};

/** A slicer whose reader is installed in range and serving. */
export const allMet: ExtensionDependencies = {
  ...none,
  core: '>=0.9',
  requires: [
    {
      module: READER,
      version: '^1.2',
      met: true,
      installed: '1.4.0',
      instance: 'sink:reader',
      state: 'ready',
    },
  ],
  suggests: [{ module: PARTS, reason: 'Shows the parts of a plan' }],
};

/** A slicer whose reader is out of range, and an old planner in conflict. */
export const unmet: ExtensionDependencies = {
  ...none,
  requires: [
    {
      module: READER,
      version: '^2.0',
      met: false,
      installed: '1.4.0',
      instance: 'sink:reader',
      state: 'ready',
    },
    {
      module: 'com.github.aux.acme.stats',
      version: '^1',
      met: false,
      installed: '1.0.0',
      instance: 'sink:stats',
      state: 'disabled',
    },
    { module: 'com.github.aux.acme.queue', version: '^1', met: false },
  ],
  suggests: [{ module: PARTS, reason: 'Shows the parts of a plan' }],
  conflicts: [
    {
      module: 'com.github.aux.acme.planner',
      version: '<2.0',
      met: false,
      installed: '1.3.0',
      instance: 'sink:planner',
      state: 'ready',
    },
    { module: 'com.github.aux.acme.legacy', version: '*', met: true },
  ],
};

/** A reader two installed extensions require. */
export const requiredBy: ExtensionDependencies = {
  ...none,
  requiredBy: [
    { instance: 'sink:slicer', module: SLICER, version: '^1.2', enabled: true },
    {
      instance: 'sink:planner',
      module: 'com.github.aux.acme.planner',
      version: '^1.0',
      enabled: true,
    },
  ],
};

/** What keeps the reader installed. */
export const whyReader: WhyCheck = {
  state: 'ready',
  reasons: [
    {
      by: SLICER,
      constraint: '^1.2',
      text: `${SLICER} 1.0.0 requires ${READER} ^1.2`,
    },
  ],
};

/** The reader has a newer release the constraints allow. */
export const updatePossible: OutdatedCheck = {
  state: 'ready',
  rows: [
    {
      namespace: READER,
      installed: '1.2.0',
      allowed: '1.4.0',
      latest: '2.0.0',
      status: 'update-possible',
    },
  ],
};

/** The reader's newer release is blocked by a constraint. */
export const blocked: OutdatedCheck = {
  state: 'ready',
  rows: [
    {
      namespace: READER,
      installed: '1.4.0',
      allowed: '1.4.0',
      latest: '2.0.0',
      status: 'blocked',
    },
  ],
};
