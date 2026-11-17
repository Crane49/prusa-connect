// What the encryption step asks, and what it adds to the confirm (ADR-20261015-ejz
// rule 36). A printer speaks TLS 1.2 with one ECDSA suite and the web app
// speaks TLS 1.3, so a printer never dials an encrypted web app: once the web
// app is encrypted, printers get a listener of their own.
import { isOwnAddress } from '../../domain/printer-address';
import { t } from '../../i18n';
import type { Encryption } from './setup-draft.svelte';

/** Whether printers need a listener apart from the web app's. */
export const needsListener = (encryption: Encryption): boolean =>
  encryption.web || encryption.printers;

const PORT = /^([^\s:]*|\[[0-9a-f:]+\]):\d{1,5}$/i;

export interface EncryptionErrors {
  web: string;
  printers: string;
  port: string;
  host: string;
}

// A printer stores this much of the hostname. Only names ending in
// prusa3d.com are shortened by the firmware and may be longer.
const MAX_HOST = 20;

/**
 * Whether a host is a bare name or address the URL parser reads back as
 * itself, so no scheme, port or path rides along, and short enough for the
 * printer to keep. The printer keeps its port apart from it.
 */
const isHostName = (host: string): boolean => {
  if (host.length > MAX_HOST && !host.endsWith('prusa3d.com')) return false;
  try {
    return new URL(`http://${host}`).hostname === host.toLowerCase();
  } catch {
    return false;
  }
};

// A name or an IPv4 address, then an optional port. Buddy printers connect
// over IPv4, so accepting an IPv6 literal would produce a settings file they
// cannot use.
const HOST_PORT = /^([^:]+)(?::(\d{1,5}))?$/;

/**
 * The host and the port printers are told to dial, split from the field. The
 * port is undefined when the field names none or the field cannot be read.
 */
export const printerHostPort = (
  field: string,
): { host: string; port: string | undefined } => {
  const match = HOST_PORT.exec(field.trim());
  if (match === null) return { host: field.trim(), port: undefined };

  return { host: match[1] ?? '', port: match[2] };
};

/** What is wrong with the address printers are given. Empty is fine. */
const hostError = (field: string): string => {
  if (field === '') return t('setupWizard.encryption.hostMissing');
  const match = HOST_PORT.exec(field);
  const host = match?.[1] ?? '';
  const port = Number(match?.[2] ?? '1');
  if (match === null || !isHostName(host) || port < 1 || port > 65535)
    return t('setupWizard.encryption.hostInvalid');
  if (isOwnAddress(host)) return t('setupWizard.encryption.hostLoopback');

  return '';
};

/**
 * What is wrong with the step's answers, field by field. Empty is fine. The
 * address printers use is asked whenever the wizard decides how printers
 * connect, which it does unless the operator's file already declares it.
 */
export const encryptionErrors = (
  encryption: Encryption,
  printersHere: boolean,
): EncryptionErrors => {
  const missingOwn = (certificate: string, key: string): boolean =>
    certificate.trim() === '' || key.trim() === '';
  const generated = encryption.made !== undefined;

  const pairError = (
    on: boolean,
    source: Encryption['webSource'],
    certificate: string,
    key: string,
  ): string => {
    if (!on) return '';
    if (source === 'generate') {
      return generated ? '' : t('setupWizard.encryption.notMade');
    }

    return missingOwn(certificate, key)
      ? t('setupWizard.encryption.missingPair')
      : '';
  };

  return {
    web: pairError(
      encryption.web,
      encryption.webSource,
      encryption.webCertificate,
      encryption.webKey,
    ),
    printers: pairError(
      encryption.printers,
      encryption.printersSource,
      encryption.printerCertificate,
      encryption.printerKey,
    ),
    port:
      needsListener(encryption) && !PORT.test(encryption.printerPort.trim())
        ? t('setupWizard.encryption.portInvalid')
        : '',
    host: printersHere ? hostError(encryption.printerHost.trim()) : '',
  };
};

/** What the step adds to the confirm. */
export interface EncryptionDecision {
  /** Answers for core.api.tls.*, laid over the person's own. */
  answers: Record<string, string>;
  printers?: { addr: string; certificate?: string; key?: string };
  /** The origin a printer dials, for core.frontend.printer_address. */
  printerAddress?: string;
}

/**
 * The confirm's part from the step's answers. `api` is the address the web
 * app listens on. `webPort` is the port through which this page reaches it,
 * which may be a container's published port instead. They are undefined when
 * the operator's file declares how printers connect, and the step then names
 * no address for them.
 */
export const encryptionDecision = (
  encryption: Encryption,
  api: string | undefined,
  webPort: string | undefined,
): EncryptionDecision => {
  const answers: Record<string, string> = {};

  if (encryption.web) {
    const [certificate, key] =
      encryption.webSource === 'generate' && encryption.made !== undefined
        ? encryption.made.web
        : [encryption.webCertificate.trim(), encryption.webKey.trim()];
    answers['core.api.tls.certificate'] = certificate;
    answers['core.api.tls.key'] = key;
  }

  // A port in the field wins over the listener's: a container published
  // under another port is reached there.
  const given = printerHostPort(encryption.printerHost);
  const host = given.host;
  const portOf = (addr: string, fallback?: string): string =>
    given.port ?? fallback ?? addr.slice(addr.lastIndexOf(':') + 1);

  if (!needsListener(encryption)) {
    return api === undefined
      ? { answers }
      : {
          answers,
          printerAddress: `http://${host}:${portOf(api.trim(), webPort)}`,
        };
  }

  const addr = encryption.printerPort.trim();
  const scheme = encryption.printers ? 'https' : 'http';
  const named =
    api === undefined
      ? {}
      : { printerAddress: `${scheme}://${host}:${portOf(addr)}` };

  if (!encryption.printers) {
    return { answers, printers: { addr }, ...named };
  }

  const [certificate, key] =
    encryption.printersSource === 'generate' && encryption.made !== undefined
      ? encryption.made.printer
      : [encryption.printerCertificate.trim(), encryption.printerKey.trim()];

  return { answers, printers: { addr, certificate, key }, ...named };
};
