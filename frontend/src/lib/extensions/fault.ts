// A refused install, update or removal, as the Extensions page says it.
//
// Most refusals read well as the registry words them. A few are written for
// somebody calling the API, and name its fields, so the page says them in the
// words of the screen instead. The code and the server's English line stay
// beside the sentence, as for every other fault (frontend-i18n.md rule 5).
import { ApiError } from '@prusa/connect';
import {
  describeFault,
  type FaultText,
  type Translate,
} from '@prusa/connect-ui/i18n';

const textParam = (thrown: unknown, name: string): string => {
  if (!(thrown instanceof ApiError)) return '';
  const value = thrown.params[name];

  return typeof value === 'string' ? value : '';
};

/** One refusal of an action on the Extensions page. */
export const describeExtensionFault = (
  thrown: unknown,
  t: Translate,
): FaultText => {
  const fault = describeFault(thrown);

  switch (fault.code) {
    case 'core.resource/MODULE_NAME_TAKEN':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.nameTaken', {
          held: textParam(thrown, 'type'),
        }),
        action: t('app.settings.extensions.fault.nameTaken.action'),
      };
    case 'core.resource/MODULE_REQUESTS_CHANGED':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.requestsChanged', {
          found: textParam(thrown, 'found'),
        }),
        action: t('app.settings.extensions.fault.requestsChanged.action'),
      };
    case 'core.resource/MODULE_DOWNLOAD_FAILED':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.downloadFailed'),
        action: t('app.settings.extensions.fault.downloadFailed.action'),
      };
    case 'core.resource/MODULE_DATABASE_NOT_MADE':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.databaseNotMade', {
          found: textParam(thrown, 'found'),
        }),
        action: t('app.settings.extensions.fault.databaseNotMade.action'),
      };
    case 'core.resource/MODULE_UPDATE_ROLLED_BACK':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.updateRolledBack', {
          version: textParam(thrown, 'version'),
          kept: textParam(thrown, 'declared'),
        }),
        action: t('app.settings.extensions.fault.updateRolledBack.action'),
      };
    case 'core.resource/MODULE_DATABASE_NOT_PURGED':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.databaseNotPurged', {
          found: textParam(thrown, 'found'),
        }),
        action: t('app.settings.extensions.fault.databaseNotPurged.action'),
      };
    // Dependencies between modules (ADR-20261027-e6k). Each refusal names
    // who needs what, in the server's words.
    case 'core.resource/MODULE_DEPENDENCY_CONFLICT':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.dependencyConflict', {
          requires: textParam(thrown, 'requires'),
        }),
        action: t('app.settings.extensions.fault.dependencyConflict.action'),
      };
    case 'core.resource/MODULE_DEPENDENCY_NOT_OFFERED':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.dependencyNotOffered', {
          requires: textParam(thrown, 'requires'),
        }),
        action: t('app.settings.extensions.fault.dependencyNotOffered.action'),
      };
    case 'core.resource/MODULE_DEPENDENCY_MISSING':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.dependencyMissing', {
          requires: textParam(thrown, 'requires'),
        }),
        action: t('app.settings.extensions.fault.dependencyMissing.action'),
      };
    case 'core.resource/MODULE_REQUIRED_BY':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.requiredBy', {
          requires: textParam(thrown, 'requires'),
        }),
        action: t('app.settings.extensions.fault.requiredBy.action'),
      };
    case 'core.resource/MODULE_FILE_TYPE_CONFLICT':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.fileTypeConflict', {
          conflicts: textParam(thrown, 'conflicts'),
        }),
        action: t('app.settings.extensions.fault.fileTypeConflict.action'),
      };
    case 'core.resource/MODULE_PLAN_CHANGED':
      return {
        ...fault,
        title: t('app.settings.extensions.fault.planChanged', {
          found: textParam(thrown, 'found'),
        }),
        action: t('app.settings.extensions.fault.planChanged.action'),
      };
    default:
      return fault;
  }
};
