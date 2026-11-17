// Whether a step may be left, worked out from the draft alone, so the screen
// and a test read the same answer.
import { t } from '../../i18n';
import type { SetupConfiguration } from '../../data';
import {
  errorOf,
  isActive,
  keysOf,
  sectionOfStep,
  shownOf,
  type StepId,
} from './model';
import { encryptionErrors } from './encryption';
import type { SetupDraft } from './setup-draft.svelte';

/** The core hashes with argon2id over at most 72 bytes, and at least 8. */
const MIN_PASSWORD = 8;
const MAX_PASSWORD = 72;

export interface IdentityErrors {
  username: string;
  password: string;
  confirmation: string;
}

/** What is wrong with the administrator, field by field. Empty is fine. */
const passwordError = (bytes: number): string => {
  if (bytes < MIN_PASSWORD) return t('setupWizard.identity.passwordShort');
  if (bytes > MAX_PASSWORD) return t('setupWizard.identity.passwordLong');

  return '';
};

export const identityErrors = (draft: SetupDraft): IdentityErrors => {
  const bytes = new TextEncoder().encode(draft.password).length;

  return {
    username:
      draft.username.trim() === ''
        ? t('setupWizard.identity.missingUsername')
        : '',
    password: passwordError(bytes),
    confirmation:
      draft.confirmation !== draft.password
        ? t('setupWizard.identity.passwordsDiffer')
        : '',
  };
};

export interface OidcErrors {
  issuer: string;
  clientId: string;
  redirectUri: string;
  subject: string;
}

/**
 * A `sub` claim is at most 255 characters (OpenID Connect Core section 2). The
 * core maps any such subject to a user id of its own, so only a space or a
 * control character is refused here.
 */
const SUBJECT = /^[\x21-\x7e\u0080-\uffff]{1,255}$/u;

const urlError = (value: string, missing: string): string => {
  if (value === '') return missing;
  try {
    const url = new URL(value);

    return url.protocol === 'https:' || url.protocol === 'http:'
      ? ''
      : t('setupWizard.oidc.notUrl');
  } catch {
    return t('setupWizard.oidc.notUrl');
  }
};

const subjectError = (subject: string): string => {
  if (subject === '') return t('setupWizard.oidc.missingSubject');

  return SUBJECT.test(subject) ? '' : t('setupWizard.oidc.badSubject');
};

/** What is wrong with the sign-in provider, field by field. Empty is fine. */
export const oidcErrors = (draft: SetupDraft): OidcErrors => {
  const { issuer, clientId, redirectUri, subject } = draft.oidc;
  return {
    issuer: urlError(issuer.trim(), t('setupWizard.oidc.missingIssuer')),
    clientId:
      clientId.trim() === '' ? t('setupWizard.oidc.missingClientId') : '',
    redirectUri:
      redirectUri.trim() === '' ? '' : urlError(redirectUri.trim(), ''),
    subject: subjectError(subject.trim()),
  };
};

const MQTT_URL = /^((tcp|mqtt|ssl|tls|mqtts):\/\/)?[^\s/:]+(:\d+)?\/?$/i;

/** What is wrong with the broker's address, or empty. */
export const mqttUrlError = (draft: SetupDraft): string =>
  draft.mqttOn && !MQTT_URL.test(draft.mqttUrl.trim())
    ? t('setupWizard.mqtt.urlInvalid')
    : '';

/** Whether the person may go on from a step. */
export const stepIsComplete = (
  step: StepId,
  draft: SetupDraft,
  contract: SetupConfiguration | undefined,
): boolean => {
  switch (step) {
    case 'code':
      return draft.code.trim() !== '';
    case 'path':
      return draft.path !== undefined;
    case 'identity': {
      if (draft.mode === 'open') return true;
      const errors =
        draft.mode === 'oidc' ? oidcErrors(draft) : identityErrors(draft);

      return Object.values(errors).every((error) => error === '');
    }
    case 'encryption':
      return Object.values(
        encryptionErrors(draft.encryption, contract?.printers === true),
      ).every((error) => error === '');
    case 'storage':
      return (
        contract?.storage.declared === true ||
        (draft.storageRoot ?? contract?.storage.root ?? '').trim() !== ''
      );
    default: {
      const section = sectionOfStep(step);
      if (section === undefined || contract === undefined) return true;
      if (section === 'network' && mqttUrlError(draft) !== '') return false;

      return keysOf(contract, section, draft.path ?? 'basic', draft.mode).every(
        (key) =>
          !isActive(key, contract, draft.answers) ||
          errorOf(key, shownOf(key, draft.answers)) === undefined,
      );
    }
  }
};

/**
 * The keys a sign-in provider sets, sent only when it is the choice. The
 * server learns it signs people in at the issuer, the first grant names the
 * administrator's subject there, and a client secret goes only when one was
 * typed, because a public client has none.
 */
export const oidcAnswers = (
  draft: SetupDraft,
  redirectFallback: string | undefined,
): Record<string, string> => {
  if (draft.mode !== 'oidc') return {};
  const { issuer, clientId, redirectUri, subject } = draft.oidc;

  return {
    'core.auth.issuer': issuer.trim(),
    'core.auth.client_id': clientId.trim(),
    'core.auth.redirect_uri':
      redirectUri.trim() === '' ? (redirectFallback ?? '') : redirectUri.trim(),
    'core.principal': subject.trim(),
    ...(draft.oidcSecret === ''
      ? {}
      : { 'core.auth.client_secret': draft.oidcSecret }),
  };
};
