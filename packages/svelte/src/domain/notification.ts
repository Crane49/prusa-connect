// Moved from pull request 185, packages/domain/src/notification.ts, 2026-09-11

/**
 * What a page tells a person about, each kind named for what happened and
 * not for the event that carried it. The comments name the SDK event type
 * a store derives each one from, under the `com.prusa.connect.` prefix.
 */
export type NotificationKind =
  /** A printer is asking a question and will not move until answered: the record's state reaching `ATTENTION`. */
  | 'printer-attention'
  /** `device.record.changed` with the state reaching `ERROR`. */
  | 'printer-error'
  /** `printer.departed`: the record is kept, the machine is not answering. */
  | 'printer-offline'
  /** `device.arrived` for a printer approved while the page was open. */
  | 'printer-connected'
  /** `device.registration.announced`: a printer waiting for someone to approve it. */
  | 'printer-pending'
  /** `device.registration.refused`: a printer this server turned away. */
  | 'printer-refused'
  /** The record's job summary reaching a finished state. */
  | 'job-finished'
  /** The record's job summary reaching a stopped or errored state. */
  | 'job-failed'
  /** `device.transfer.settled` without the file landing. */
  | 'transfer-failed'
  /** `resource.state.changed` with the result `switched-off-by-host`: the server stopped restarting a module that kept failing. */
  | 'module-switched-off';

export type NotificationTone = 'critical' | 'attention' | 'success' | 'info';

const NOTIFICATION_TONE: Record<NotificationKind, NotificationTone> = {
  'printer-attention': 'attention',
  'printer-error': 'critical',
  'printer-offline': 'attention',
  'printer-connected': 'info',
  'printer-pending': 'attention',
  'printer-refused': 'attention',
  'job-finished': 'success',
  'job-failed': 'critical',
  'transfer-failed': 'critical',
  'module-switched-off': 'critical',
};

/**
 * Priority, not chronology: what needs doing sorts above what merely
 * happened, because this list is read to find out what to do next and not to
 * reconstruct a morning. `success` and `info` share the bottom; neither asks
 * for anything, they differ in what they report.
 */
const NOTIFICATION_TONE_RANK: Record<NotificationTone, number> = {
  critical: 0,
  attention: 1,
  success: 2,
  info: 3,
};

export const toneOf = (kind: NotificationKind): NotificationTone =>
  NOTIFICATION_TONE[kind];

export const compareByTone = (
  a: NotificationKind,
  b: NotificationKind,
): number =>
  NOTIFICATION_TONE_RANK[toneOf(a)] - NOTIFICATION_TONE_RANK[toneOf(b)];

/** How many a person has not looked at: what decides whether the bell is marked. */
export const unreadCount = (
  items: readonly { read?: boolean | undefined }[],
): number => items.filter((item) => item.read !== true).length;

/**
 * The loudest tone among the unread, or `undefined` when there is nothing
 * unread. The bell's own mark takes its colour from this: one printer in
 * error under nine finished prints is still an error.
 */
export const unreadTone = (
  items: readonly { kind: NotificationKind; read?: boolean | undefined }[],
): NotificationTone | undefined =>
  items
    .filter((item) => item.read !== true)
    .map((item) => toneOf(item.kind))
    .sort((a, b) => NOTIFICATION_TONE_RANK[a] - NOTIFICATION_TONE_RANK[b])[0];
