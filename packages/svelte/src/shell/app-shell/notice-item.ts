// A notice from the bell's store in the words the bell draws.
import type { Notice } from '../../data';
import { formatDate, t } from '../../i18n';
import {
  type NotificationAction,
  type NotificationItem,
  toast,
} from '../../primitives';

/** Where a notice leads, as the host's routes name it. */
export type NoticeHref = (notice: Notice) => string | undefined;

/** The sentence the device-issue vocabulary gives a reason, once it is read. */
export type IssueSummary = (reason: string) => string | undefined;

const titleOf = (notice: Notice): string => {
  const printer = notice.printer;

  switch (notice.kind) {
    case 'job-finished':
      return t('shell.notice.jobFinished.title', { printer });
    case 'job-failed':
      return t('shell.notice.jobFailed.title', { printer });
    case 'printer-offline':
      return t('shell.notice.offline.title', { printer });
    case 'printer-pending':
      return printer === ''
        ? t('shell.notice.pendingUnknown.title')
        : t('shell.notice.pending.title', { printer });
    case 'printer-connected':
      return t('shell.notice.connected.title', { printer });
    case 'printer-refused':
      return printer === ''
        ? t('shell.notice.refusedUnknown.title')
        : t('shell.notice.refused.title', { printer });
    case 'module-switched-off':
      return t('shell.notice.moduleSwitchedOff.title', {
        module: notice.module ?? '',
      });
    default:
      return t('shell.notice.issue.title', { printer });
  }
};

const bodyOf = (
  notice: Notice,
  issueSummary: IssueSummary | undefined,
): string | undefined => {
  if (notice.kind === 'printer-pending' && notice.code !== undefined) {
    return t('shell.notice.pending.body', { code: notice.code });
  }
  if (notice.kind === 'printer-refused') {
    return notice.reason === 'not_admitted'
      ? t('shell.notice.refused.notAdmitted')
      : t('shell.notice.refused.unknownCredentials');
  }

  if (notice.kind === 'module-switched-off') {
    return t('shell.notice.moduleSwitchedOff.body');
  }

  if (notice.kind === 'printer-error' || notice.kind === 'printer-attention') {
    // The token is for code to branch on, never for a person to read.
    return (
      (notice.issue === undefined ? undefined : issueSummary?.(notice.issue)) ??
      t('shell.notice.issue.body')
    );
  }

  return notice.file;
};

const ACTION_LABEL: Partial<Record<Notice['kind'], string>> = {
  'printer-pending': 'shell.notice.action.review',
  'printer-connected': 'shell.notice.action.open',
  'printer-refused': 'shell.notice.action.addPrinter',
  'printer-error': 'shell.notice.action.open',
  'printer-attention': 'shell.notice.action.open',
  'printer-offline': 'shell.notice.action.open',
  'job-finished': 'shell.notice.action.openPrint',
  'job-failed': 'shell.notice.action.openPrint',
  'module-switched-off': 'shell.notice.action.openModule',
};

const actionsOf = (
  notice: Notice,
  href: string | undefined,
): NotificationAction[] => {
  const label = ACTION_LABEL[notice.kind];
  if (href === undefined || label === undefined) return [];

  return [{ id: 'open', label: t(label), href }];
};

export const noticeItem = (
  notice: Notice,
  href?: string,
  issueSummary?: IssueSummary,
): NotificationItem => {
  const body = bodyOf(notice, issueSummary);
  const actions = actionsOf(notice, href);

  return {
    id: notice.id,
    kind: notice.kind,
    title: titleOf(notice),
    ...(body === undefined ? {} : { body }),
    ...(actions.length === 0 ? {} : { actions }),
    at: formatDate(notice.at, { timeStyle: 'short' }),
    read: notice.read,
  };
};

/**
 * Raises a toast for what a person should not have to open the bell to
 * learn: a print that failed, a module the server switched off, and a printer
 * waiting for approval. A refused printer stays in the bell: the caller
 * chooses what it claims to be, so a toast would let anybody on the network
 * put text on the screen.
 */
export const announceNotice = (notice: Notice): void => {
  const item = noticeItem(notice);
  const options = item.body === undefined ? {} : { description: item.body };

  switch (notice.kind) {
    case 'job-failed':
    case 'module-switched-off':
      toast.error(item.title, options);
      break;
    case 'printer-pending':
      toast.warning(item.title, options);
      break;
    default:
      break;
  }
};
