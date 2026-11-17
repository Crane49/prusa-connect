// The text a failed Send to Prusa is shown with. The portal's own code and
// English message go under it, the way a fault of the core is shown.
import { DiagnosticsSendError } from '../../data/services/send-diagnostics';
import { describeFault, type FaultText, formatDuration, t } from '../../i18n';

/** Describes a failure from the portal, or any other fault as the core's. */
export const describeSendFailure = (thrown: unknown): FaultText => {
  if (!(thrown instanceof DiagnosticsSendError)) return describeFault(thrown);

  const detail = thrown.message === '' ? undefined : thrown.message;
  const code = thrown.code;

  switch (thrown.status) {
    case 0:
      return {
        code,
        detail,
        title: t('settingsDiagnostics.sendFailed.unreachable'),
      };
    case 404:
      return {
        code,
        detail,
        title: t('settingsDiagnostics.sendFailed.product'),
      };
    case 413:
      return {
        code,
        detail,
        title: t('settingsDiagnostics.sendFailed.tooLarge'),
      };
    case 415:
      return {
        code,
        detail,
        title: t('settingsDiagnostics.sendFailed.notZip'),
      };
    case 429:
      return {
        code,
        detail,
        title:
          thrown.retryAfter === undefined
            ? t('settingsDiagnostics.sendFailed.busy')
            : t('settingsDiagnostics.sendFailed.busyFor', {
                wait: formatDuration(thrown.retryAfter),
              }),
      };
    default:
      return { code, detail, title: t('settingsDiagnostics.sendFailed.other') };
  }
};
