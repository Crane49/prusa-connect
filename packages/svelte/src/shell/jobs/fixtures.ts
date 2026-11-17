// Sample prints for this screen's stories and its browser test, so both read
// the same list and a story that drifts from a test is a change to one file.
import type { ListingStore } from '../../primitives';
import type { JobRecord, JobsPrinter, JobsSource } from './types';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

/** Fixed, never `Date.now()`, so a screenshot and an assertion do not drift. */
const NOW = Date.parse('2026-08-19T10:00:00Z');

const PRINTERS: readonly { readonly id: string; readonly name: string }[] = [
  { id: 'prn-rack-3', name: 'MK4S, print farm rack 3' },
  { id: 'prn-core', name: 'CORE One' },
  { id: 'prn-xl', name: 'XL Multi-Tool' },
  { id: 'prn-mini', name: 'MINI+' },
  { id: 'prn-mk3s', name: 'MK3S+ (storage room)' },
];

interface Sample {
  printer: string;
  number: number;
  name: string;
  state: string;
  /** How long before `NOW` the print started. */
  startedHoursAgo: number;
  /** How long it ran. `null` for a print still on the bed. */
  ranMinutes: number | null;
  progress: number;
}

const recordOf = (sample: Sample): JobRecord => {
  const started = NOW - sample.startedHoursAgo * HOUR;

  return {
    id: `${sample.printer}:${String(sample.number)}`,
    display_name: sample.name,
    path: `/usb/${sample.name}`,
    state: sample.state,
    progress: sample.progress,
    started: new Date(started).toISOString(),
    ...(sample.ranMinutes === null
      ? {}
      : {
          ended: new Date(started + sample.ranMinutes * MINUTE).toISOString(),
        }),
  };
};

/**
 * Twelve prints over five printers: enough to draw three pages at the
 * smallest page size, enough that every state chip matches something, two
 * prints sharing a file name on different printers, one name long enough to
 * clip at every width, and the two running prints mid-list rather than first.
 */
const SAMPLES: readonly Sample[] = [
  {
    printer: 'prn-rack-3',
    number: 41,
    name: 'Bracket_Left_Reinforced_v4_FINAL.gcode',
    state: 'FINISHED',
    startedHoursAgo: 3,
    ranMinutes: 128,
    progress: 100,
  },
  {
    printer: 'prn-rack-3',
    number: 42,
    name: 'Camera_Mount_Bracket_v3.gcode',
    state: 'PRINTING',
    startedHoursAgo: 1,
    ranMinutes: null,
    progress: 42,
  },
  {
    printer: 'prn-core',
    number: 17,
    name: 'enclosure_frame_final_v6_verified_assembly_left.gcode',
    state: 'ERROR',
    startedHoursAgo: 4,
    ranMinutes: 6,
    progress: 3,
  },
  {
    printer: 'prn-mini',
    number: 8,
    name: 'benchy.3mf',
    state: 'FINISHED',
    startedHoursAgo: 12,
    ranMinutes: 41,
    progress: 100,
  },
  {
    printer: 'prn-xl',
    number: 5,
    name: 'Articulated_Dragon_Body_Support_Structure.bgcode',
    state: 'PRINTING',
    startedHoursAgo: 13,
    ranMinutes: null,
    progress: 73,
  },
  {
    printer: 'prn-core',
    number: 16,
    name: 'benchy.3mf',
    state: 'STOPPED',
    startedHoursAgo: 15,
    ranMinutes: 3,
    progress: 4,
  },
  {
    printer: 'prn-mk3s',
    number: 90,
    name: 'Test_Cube_20mm.gcode',
    state: 'FINISHED',
    startedHoursAgo: 17,
    ranMinutes: 22,
    progress: 100,
  },
  {
    printer: 'prn-mini',
    number: 7,
    name: 'nozzle_holder_v2.gcode',
    state: 'ERROR',
    startedHoursAgo: 20,
    ranMinutes: 64,
    progress: 55,
  },
  {
    printer: 'prn-rack-3',
    number: 40,
    name: 'Spool_Holder_Left.gcode',
    state: 'FINISHED',
    startedHoursAgo: 46,
    ranMinutes: 231,
    progress: 100,
  },
  {
    printer: 'prn-xl',
    number: 4,
    name: 'calibration_cube_pla.gcode',
    state: 'STOPPED',
    startedHoursAgo: 48,
    ranMinutes: 12,
    progress: 19,
  },
  {
    printer: 'prn-core',
    number: 15,
    name: 'Fan_Duct_Rev8.gcode',
    state: 'FINISHED',
    startedHoursAgo: 61,
    ranMinutes: 93,
    progress: 100,
  },
  {
    printer: 'prn-mk3s',
    number: 89,
    name: 'gasket_set_tpu.gcode',
    state: 'FINISHED',
    startedHoursAgo: 73,
    ranMinutes: 58,
    progress: 100,
  },
];

const printersWith = (
  jobsFor: (id: string) => readonly JobRecord[] | undefined,
): JobsPrinter[] =>
  PRINTERS.map((printer) => ({ ...printer, jobs: jobsFor(printer.id) }));

const recordsFor = (id: string): JobRecord[] =>
  SAMPLES.filter((sample) => sample.printer === id).map(recordOf);

const source = (printers: JobsPrinter[], read = true): JobsSource => ({
  printers,
  loading: !read,
  loaded: read,
  error: undefined,
});

/** Every printer answered. */
export const jobsRead = (): JobsSource => source(printersWith(recordsFor));

/** A fresh instance: printers registered, nothing printed yet. */
export const nothingPrinted = (): JobsSource => source(printersWith(() => []));

/** Before the first answer lands. */
export const stillReading = (): JobsSource =>
  source(
    printersWith(() => undefined),
    false,
  );

/** The server refused, with a code from the fault registry. */
export const readRefused = (): JobsSource => ({
  printers: [],
  loading: false,
  loaded: false,
  error: {
    code: 'core.auth/ACCESS_DENIED',
    message: 'core.auth/ACCESS_DENIED: no grant on device prn-core',
  },
});

/**
 * A store that answers one remembered choice and forgets what is written, so a
 * story or a test can open the screen on a view, a page size or a narrowed set
 * of fields without pressing its way there first.
 */
export const remembering = (held: {
  fields?: string[];
  view?: string;
  pageSize?: number;
}): ListingStore => ({
  read: (key) => (key === 'listing.jobs' ? JSON.stringify(held) : null),
  write: () => undefined,
});
