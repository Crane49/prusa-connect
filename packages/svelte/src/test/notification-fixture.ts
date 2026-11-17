import type { NotificationItem } from '../primitives/notifications';

/**
 * One of each tone, so the four are readable side by side rather than one at a
 * time: a stopped printer, one waiting for a person, a print that finished,
 * and a machine that came back on the network. Two rows carry the printer's
 * own artwork and one the plate it was printing, which is what a row leads
 * with when it has either.
 *
 * Every row names a kind the backend emits. There is no level to pick, which
 * is what stops a fixture inventing an urgency no event behind the UI can
 * raise.
 */
export const NOTIFICATION_FIXTURE: NotificationItem[] = [
  {
    id: 'error',
    kind: 'printer-error',
    title: 'MK4S — Print Farm Rack 3 stopped',
    body: 'Thermal runaway detected on the nozzle heater.',
    at: '09:41',
    read: false,
    printerModel: 'mk4s',
    actions: [
      { id: 'view', label: 'View printer', variant: 'secondary' },
      { id: 'ack', label: 'Acknowledge', variant: 'secondary' },
    ],
  },
  {
    id: 'attention',
    kind: 'printer-attention',
    title: 'CORE One is waiting for a person',
    body: 'Is the filament loaded? — asked at 09:12.',
    at: '09:12',
    read: false,
    printerModel: 'core_one',
    actions: [{ id: 'view', label: 'Answer', variant: 'secondary' }],
  },
  {
    id: 'done',
    kind: 'job-finished',
    title: 'bracket_v4.gcode finished on XL 5T',
    at: '07:58',
    read: true,
    thumbnailUrl: '/img/files/bracket-array.png',
  },
  {
    id: 'info',
    kind: 'printer-connected',
    title: 'MINI+ 04 is back on the network',
    body: 'Reporting telemetry again from 10.24.6.31.',
    at: 'Yesterday',
    read: true,
  },
];

/**
 * Longer than any panel shows at once. Built from the fixture rather than
 * written out, because what matters is the count and the read and unread mix,
 * not sixteen distinct sentences.
 */
export const LONG_NOTIFICATION_FIXTURE: NotificationItem[] = Array.from(
  { length: 4 },
  (_unused, round) =>
    NOTIFICATION_FIXTURE.map((item) => ({
      ...item,
      id: `${item.id}-${String(round)}`,
      read: round > 0 ? true : item.read,
    })),
).flat();
