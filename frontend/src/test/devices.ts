import type { Device } from '@prusa/connect-ui/domain';

// A device record with every member the document requires, so a test names
// only the two or three fields the assertion is about.
const BASE: Device = {
  actions: ['observe', 'operate'],
  authorization_epoch: 1,
  capabilities: ['core.command.pause_print', 'core.command.stop_print'],
  capability_epoch: 1,
  connected: true,
  driver: 'prusalink',
  firmware: '6.2.4',
  id: 'p1',
  issues: [],
  kind: 'printer',
  labels: {},
  link: {
    counted: true,
    dropped_telemetry: 0,
    dropped_telemetry_per_minute: 0,
    measured_over_seconds: 60,
    refused_frames: 0,
    refused_frames_per_minute: 0,
  },
  model: 'MK4',
  name: 'Bench one',
  profile_matched: true,
  serial: 'SN-1',
  state: { '/state': 'IDLE' },
  unit_uid: 'unit-1',
  vendor: 'Prusa',
  verbs: null,
};

export const device = (over: Partial<Device> = {}): Device => ({
  ...BASE,
  ...over,
});

/** The listing `GET /api/v1/devices` answers for a set of rows. */
export const page = (rows: readonly Device[]): Record<string, unknown> => ({
  items: rows,
  limit: 100,
  offset: 0,
  total: rows.length,
  issues: { affected: 0, counts: {} },
});
