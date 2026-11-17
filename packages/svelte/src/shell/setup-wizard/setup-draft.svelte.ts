// What a person has typed into the first-run wizard, kept across a reload
// (ADR-20261015-ejz rule 30).
import {
  browserStorage,
  browserTabStorage,
  readRemembered,
  type RememberedStorage,
  writeRemembered,
} from '../../remembered';
import { type Answer, isStep, type SetupPath, type StepId } from './model';

/** Where the answers are kept: this browser, for as long as it keeps them. */
export const DRAFT_KEY = 'prusa-connect.setup.draft';
/** Where the password and the code wait: this tab, until it is closed. */
export const SECRET_KEY = 'prusa-connect.setup.secret';

export type SetupMode = 'required' | 'open' | 'oidc';
export type SetupAdmission = 'ask' | 'open';

interface Kept {
  step: StepId;
  /** Every step the person has opened, so they may open it again at once. */
  visited: StepId[];
  path: SetupPath | undefined;
  mode: SetupMode;
  username: string;
  oidc: Oidc;
  admission: SetupAdmission;
  storageRoot: string | undefined;
  answers: Record<string, Answer>;
  mqttOn: boolean;
  mqttUrl: string;
  mqttUser: string;
  encryption: Encryption;
  historyOn: boolean;
  historyRetention: string;
}

/** A sign-in provider people already run, over OpenID Connect. */
export interface Oidc {
  issuer: string;
  clientId: string;
  /** Empty sends the address the wizard works out from this page. */
  redirectUri: string;
  /** The administrator's subject at the issuer, its `sub` claim. */
  subject: string;
}

export const NO_OIDC: Oidc = {
  issuer: '',
  clientId: '',
  redirectUri: '',
  subject: '',
};

/** Where a certificate comes from: made by the server, or the person's own. */
export type CertificateSource = 'generate' | 'own';

/** The encryption step's answers. Paths only; no key ever reaches the page. */
export interface Encryption {
  web: boolean;
  webSource: CertificateSource;
  webCertificate: string;
  webKey: string;
  printers: boolean;
  printersSource: CertificateSource;
  printerCertificate: string;
  printerKey: string;
  /** The port printers dial, written as :8443. */
  printerPort: string;
  /** The name or address printers reach this server at. */
  printerHost: string;
  /** The names the server's certificates are made for, one per line. */
  names: string;
  /** The files the server made, so a reload still knows them. */
  made: { web: [string, string]; printer: [string, string] } | undefined;
}

export const NO_ENCRYPTION: Encryption = {
  web: false,
  webSource: 'generate',
  webCertificate: '',
  webKey: '',
  printers: false,
  printersSource: 'generate',
  printerCertificate: '',
  printerKey: '',
  printerPort: ':8443',
  printerHost: '',
  names: '',
  made: undefined,
};

interface Secrets {
  code: string;
  password: string;
  confirmation: string;
  mqttPassword: string;
  oidcSecret: string;
}

const EMPTY: Kept = {
  step: 'welcome',
  visited: ['welcome'],
  path: undefined,
  mode: 'required',
  username: '',
  oidc: { ...NO_OIDC },
  admission: 'ask',
  storageRoot: undefined,
  answers: {},
  mqttOn: false,
  mqttUrl: '',
  mqttUser: '',
  encryption: { ...NO_ENCRYPTION },
  historyOn: true,
  historyRetention: '168h',
};

const NO_SECRETS: Secrets = {
  code: '',
  password: '',
  confirmation: '',
  mqttPassword: '',
  oidcSecret: '',
};

// Both documents are read back from storage a person or an extension may have
// written, which is a trust boundary (typescript.md): each member is checked
// and a wrong one falls back to where a new wizard starts.
const recordOf = (raw: string | null): Record<string, unknown> => {
  if (raw === null || raw === '') return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return {};

    return { ...parsed };
  } catch {
    return {};
  }
};

const textOf = (value: unknown, fallback: string): string =>
  typeof value === 'string' ? value : fallback;

const answersOf = (value: unknown): Record<string, Answer> => {
  if (typeof value !== 'object' || value === null) return {};
  const out: Record<string, Answer> = {};
  for (const [path, answer] of Object.entries(value)) {
    if (typeof answer === 'string' || typeof answer === 'boolean') {
      out[path] = answer;
    }
  }

  return out;
};

const sourceOf = (value: unknown): CertificateSource =>
  value === 'own' ? 'own' : 'generate';

const pairOf = (value: unknown): [string, string] | undefined =>
  Array.isArray(value) &&
  value.length === 2 &&
  typeof value[0] === 'string' &&
  typeof value[1] === 'string'
    ? [value[0], value[1]]
    : undefined;

const encryptionOf = (value: unknown): Encryption => {
  if (typeof value !== 'object' || value === null) return { ...NO_ENCRYPTION };
  const read: Record<string, unknown> = { ...value };
  const made: Record<string, unknown> =
    typeof read.made === 'object' && read.made !== null ? { ...read.made } : {};
  const web = pairOf(made.web);
  const printer = pairOf(made.printer);

  return {
    web: read.web === true,
    webSource: sourceOf(read.webSource),
    webCertificate: textOf(read.webCertificate, ''),
    webKey: textOf(read.webKey, ''),
    printers: read.printers === true,
    printersSource: sourceOf(read.printersSource),
    printerCertificate: textOf(read.printerCertificate, ''),
    printerKey: textOf(read.printerKey, ''),
    printerPort: textOf(read.printerPort, NO_ENCRYPTION.printerPort),
    printerHost: textOf(read.printerHost, ''),
    names: textOf(read.names, ''),
    made:
      web !== undefined && printer !== undefined ? { web, printer } : undefined,
  };
};

const oidcOf = (value: unknown): Oidc => {
  if (typeof value !== 'object' || value === null) return { ...NO_OIDC };
  const read: Record<string, unknown> = { ...value };

  return {
    issuer: textOf(read.issuer, ''),
    clientId: textOf(read.clientId, ''),
    redirectUri: textOf(read.redirectUri, ''),
    subject: textOf(read.subject, ''),
  };
};

// A draft written before the wizard kept the steps seen has none, so only
// its own step counts as seen.
const visitedOf = (value: unknown, step: StepId): StepId[] => {
  const visited = Array.isArray(value) ? value.filter(isStep) : [];

  return visited.includes(step) ? visited : [...visited, step];
};

const keptOf = (raw: string | null): Kept => {
  const read = recordOf(raw);
  const step = isStep(read.step) ? read.step : EMPTY.step;

  return {
    step,
    visited: visitedOf(read.visited, step),
    path:
      read.path === 'basic' || read.path === 'advanced' ? read.path : undefined,
    mode: read.mode === 'open' || read.mode === 'oidc' ? read.mode : 'required',
    username: textOf(read.username, ''),
    oidc: oidcOf(read.oidc),
    admission: read.admission === 'open' ? 'open' : 'ask',
    storageRoot:
      typeof read.storageRoot === 'string' ? read.storageRoot : undefined,
    answers: answersOf(read.answers),
    mqttOn: read.mqttOn === true,
    mqttUrl: textOf(read.mqttUrl, ''),
    mqttUser: textOf(read.mqttUser, ''),
    encryption: encryptionOf(read.encryption),
    historyOn: read.historyOn !== false,
    historyRetention: textOf(read.historyRetention, EMPTY.historyRetention),
  };
};

const secretsOf = (raw: string | null): Secrets => {
  const read = recordOf(raw);

  return {
    code: textOf(read.code, ''),
    password: textOf(read.password, ''),
    confirmation: textOf(read.confirmation, ''),
    mqttPassword: textOf(read.mqttPassword, ''),
    oidcSecret: textOf(read.oidcSecret, ''),
  };
};

/**
 * The wizard's own state, so a store of its own (svelte-5.md). Every setter
 * writes through, so a reload at any moment finds what was typed. The answers
 * go to `localStorage`; the password and the code go to `sessionStorage`
 * only, which is gone with the tab.
 */
export class SetupDraft {
  readonly #local: RememberedStorage | null;
  readonly #tab: RememberedStorage | null;
  #kept = $state<Kept>({ ...EMPTY });
  #secrets = $state<Secrets>({ ...NO_SECRETS });

  constructor(
    local: RememberedStorage | null = browserStorage(),
    tab: RememberedStorage | null = browserTabStorage(),
  ) {
    this.#local = local;
    this.#tab = tab;
    this.#kept = keptOf(readRemembered(local, DRAFT_KEY));
    this.#secrets = secretsOf(readRemembered(tab, SECRET_KEY));
  }

  get step(): StepId {
    return this.#kept.step;
  }

  set step(step: StepId) {
    const { visited } = this.#kept;
    this.#keep({
      step,
      visited: visited.includes(step) ? visited : [...visited, step],
    });
  }

  /** Every step the person has opened. Set through `step` only. */
  get visited(): readonly StepId[] {
    return this.#kept.visited;
  }

  get path(): SetupPath | undefined {
    return this.#kept.path;
  }

  set path(path: SetupPath | undefined) {
    this.#keep({ path });
  }

  get mode(): SetupMode {
    return this.#kept.mode;
  }

  set mode(mode: SetupMode) {
    this.#keep({ mode });
  }

  get username(): string {
    return this.#kept.username;
  }

  set username(username: string) {
    this.#keep({ username });
  }

  /** The sign-in provider's answers. Read-only by design: see sso(). */
  get oidc(): Readonly<Oidc> {
    return this.#kept.oidc;
  }

  /** Changes some of the sign-in provider's answers. */
  sso(change: Partial<Oidc>): void {
    this.#keep({ oidc: { ...this.#kept.oidc, ...change } });
  }

  /**
   * The provider's client secret, beside the other secrets in this tab.
   * Empty is a public client.
   */
  get oidcSecret(): string {
    return this.#secrets.oidcSecret;
  }

  set oidcSecret(oidcSecret: string) {
    this.#hide({ oidcSecret });
  }

  get admission(): SetupAdmission {
    return this.#kept.admission;
  }

  set admission(admission: SetupAdmission) {
    this.#keep({ admission });
  }

  /** The folder for uploads, `undefined` until the person names one. */
  get storageRoot(): string | undefined {
    return this.#kept.storageRoot;
  }

  set storageRoot(storageRoot: string | undefined) {
    this.#keep({ storageRoot });
  }

  /** The configuration answers, by dotted path. Read-only by design. */
  get answers(): Readonly<Record<string, Answer>> {
    return this.#kept.answers;
  }

  get code(): string {
    return this.#secrets.code;
  }

  set code(code: string) {
    this.#hide({ code });
  }

  get password(): string {
    return this.#secrets.password;
  }

  set password(password: string) {
    this.#hide({ password });
  }

  get confirmation(): string {
    return this.#secrets.confirmation;
  }

  set confirmation(confirmation: string) {
    this.#hide({ confirmation });
  }

  /** Whether the printers' state is sent to an MQTT broker. */
  get mqttOn(): boolean {
    return this.#kept.mqttOn;
  }

  set mqttOn(mqttOn: boolean) {
    this.#keep({ mqttOn });
  }

  get mqttUrl(): string {
    return this.#kept.mqttUrl;
  }

  set mqttUrl(mqttUrl: string) {
    this.#keep({ mqttUrl });
  }

  get mqttUser(): string {
    return this.#kept.mqttUser;
  }

  set mqttUser(mqttUser: string) {
    this.#keep({ mqttUser });
  }

  /** The broker's password waits beside the other secrets, in this tab. */
  get mqttPassword(): string {
    return this.#secrets.mqttPassword;
  }

  set mqttPassword(mqttPassword: string) {
    this.#hide({ mqttPassword });
  }

  /** The encryption step's answers. Read-only by design: see encrypt(). */
  get encryption(): Readonly<Encryption> {
    return this.#kept.encryption;
  }

  /** Changes some of the encryption step's answers. */
  encrypt(change: Partial<Encryption>): void {
    this.#keep({ encryption: { ...this.#kept.encryption, ...change } });
  }

  /** Whether the printers' charts, events and command log are kept. */
  get historyOn(): boolean {
    return this.#kept.historyOn;
  }

  set historyOn(historyOn: boolean) {
    this.#keep({ historyOn });
  }

  /** How long a sample is kept, as a duration; "0" keeps every sample. */
  get historyRetention(): string {
    return this.#kept.historyRetention;
  }

  set historyRetention(historyRetention: string) {
    this.#keep({ historyRetention });
  }

  /** Records one configuration answer. */
  answer(path: string, value: Answer): void {
    this.#keep({ answers: { ...this.#kept.answers, [path]: value } });
  }

  /** Forgets one answer, so the key goes back to where it started. */
  forget(path: string): void {
    const answers = Object.fromEntries(
      Object.entries(this.#kept.answers).filter(([key]) => key !== path),
    );
    this.#keep({ answers });
  }

  /** Forgets everything, once the server has recorded the decision. */
  clear(): void {
    this.#kept = { ...EMPTY };
    this.#secrets = { ...NO_SECRETS };
    writeRemembered(this.#local, DRAFT_KEY, '');
    writeRemembered(this.#tab, SECRET_KEY, '');
  }

  #keep(change: Partial<Kept>): void {
    this.#kept = { ...this.#kept, ...change };
    writeRemembered(this.#local, DRAFT_KEY, JSON.stringify(this.#kept));
  }

  #hide(change: Partial<Secrets>): void {
    this.#secrets = { ...this.#secrets, ...change };
    writeRemembered(this.#tab, SECRET_KEY, JSON.stringify(this.#secrets));
  }
}
