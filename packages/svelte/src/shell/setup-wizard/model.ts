// What the first-run wizard asks and in which order, worked out from the
// configuration contract the server sends (ADR-20261015-ejz rules 10 to 15, 28, 29).
import type { SetupConfiguration, SetupKey } from '../../data';
import type { SetupMode } from './setup-draft.svelte';

/** The short path asks the basic keys, the long one asks every key. */
export type SetupPath = 'basic' | 'advanced';

/** The sections the configuration steps draw, each a group of key groups. */
export const SECTIONS = [
  'network',
  'look',
  'access',
  'database',
  'logging',
  'server',
] as const;

export type SectionId = (typeof SECTIONS)[number];

/** Every step the wizard can show. */
export type StepId =
  | 'welcome'
  | 'code'
  | 'path'
  | 'identity'
  | 'admission'
  | 'storage'
  | 'encryption'
  | `section:${SectionId}`
  | 'summary';

/** What a person typed into one field: text, or a switch's position. */
export type Answer = string | boolean;

const SECTION_OF_GROUP: Readonly<Record<string, SectionId>> = {
  api: 'network',
  discovery: 'network',
  mqtt: 'network',
  events: 'network',
  frontend: 'look',
  dashboards: 'look',
  marketplace: 'look',
  auth: 'access',
  authz: 'access',
  database: 'database',
  log: 'logging',
  observability: 'logging',
};

/**
 * The keys the encryption step sets, which no section draws. The step writes
 * them from its own answers.
 */
export const ENCRYPTION_KEYS: readonly string[] = [
  'core.api.tls.certificate',
  'core.api.tls.key',
];

/**
 * The keys the identity step sets, which no section draws. A sign-in
 * provider and the administrator's subject at it are one decision, asked
 * beside the other ways people sign in. The password login is that choice
 * too: "everybody signs in" turns it on, and the switch beside the other two
 * choices could only be refused.
 */
export const IDENTITY_KEYS: readonly string[] = [
  'core.auth.local',
  'core.auth.issuer',
  'core.auth.client_id',
  'core.auth.client_secret',
  'core.auth.redirect_uri',
  'core.principal',
];

/**
 * The keys that set how much work checking one password takes. They mean
 * something only when people sign in with a password stored on this server.
 */
export const PASSWORD_KEYS: readonly string[] = [
  'core.auth.password_memory_bytes',
  'core.auth.password_passes',
  'core.auth.password_hashes_at_once',
];

/** Whether the identity step's choice leaves a key nothing to do. */
const idle = (key: SetupKey, mode: SetupMode | undefined): boolean =>
  PASSWORD_KEYS.includes(key.path) && mode !== undefined && mode !== 'required';

/** Where the issuer sends the browser back to, on this server. */
export const OIDC_CALLBACK = '/api/v1/auth/callback';

/** The section a key is drawn in. A group the page does not know is server. */
export const sectionOf = (key: SetupKey): SectionId =>
  SECTION_OF_GROUP[key.group] ?? 'server';

/** The keys one section asks on one path, under the identity step's choice. */
export const keysOf = (
  contract: SetupConfiguration,
  section: SectionId,
  path: SetupPath,
  mode?: SetupMode,
): SetupKey[] =>
  (contract.keys ?? []).filter(
    (key) =>
      sectionOf(key) === section &&
      !ENCRYPTION_KEYS.includes(key.path) &&
      !IDENTITY_KEYS.includes(key.path) &&
      !idle(key, mode) &&
      (path === 'advanced' || key.level === 'basic'),
  );

/**
 * The steps in order. Until the code is accepted the wizard knows only the
 * first three, and a storage the operator's file already declares is not
 * asked again.
 */
export const stepsOf = (
  contract: SetupConfiguration | undefined,
  path: SetupPath | undefined,
  mode?: SetupMode,
): StepId[] => {
  const steps: StepId[] = ['welcome', 'code'];
  if (contract === undefined) return steps;

  steps.push('path');
  if (path === undefined) return steps;

  steps.push('identity', 'admission');
  // The step is offered even when the file declares a storage: it also keeps
  // each printer's history.
  steps.push('storage');
  steps.push('encryption');

  for (const section of SECTIONS) {
    if (keysOf(contract, section, path, mode).length > 0) {
      steps.push(`section:${section}`);
    }
  }

  steps.push('summary');

  return steps;
};

/** The section a step draws, or undefined for a step that is not one. */
export const sectionOfStep = (step: StepId): SectionId | undefined =>
  SECTIONS.find((section) => step === `section:${section}`);

/**
 * The furthest step the person may open again, counted from zero: the last
 * step of the run, from the first, that they have all seen. A step a new path
 * or sign-in mode adds has not been seen, so it ends the run there. The step
 * they are on is always reached.
 */
export const reachedOf = (
  steps: readonly StepId[],
  visited: readonly StepId[],
  current: number,
): number => {
  const unseen = steps.findIndex((step) => !visited.includes(step));
  const run = unseen === -1 ? steps.length - 1 : unseen - 1;

  return Math.max(run, current);
};

/** Whether a string names a step, for a draft read back from storage. */
export const isStep = (value: unknown): value is StepId =>
  typeof value === 'string' &&
  ([
    'welcome',
    'code',
    'path',
    'identity',
    'admission',
    'storage',
    'encryption',
    'summary',
  ].includes(value) ||
    SECTIONS.some((section) => value === `section:${section}`));

/** The value a key starts from in a field: the operator's, or the default. */
export const startOf = (key: SetupKey): Answer => {
  const value = key.locked ? key.value : key.default;
  if (key.kind === 'switch') return value === true;
  if (Array.isArray(value)) return value.map(String).join('\n');
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }

  return '';
};

/** What the field shows: the person's answer, or where the key starts. */
export const shownOf = (
  key: SetupKey,
  answers: Readonly<Record<string, Answer>>,
): Answer => (key.locked ? startOf(key) : (answers[key.path] ?? startOf(key)));

/**
 * Whether a key is on: it names no key that turns it on, or that key is on
 * itself and holds the value this one asks for. A switch turns its keys on
 * when it is on, a closed set when it holds the named value, and a text key
 * when it is not empty.
 */
export const isActive = (
  key: SetupKey,
  contract: SetupConfiguration,
  answers: Readonly<Record<string, Answer>>,
): boolean => {
  if (key.depends_on === undefined || key.depends_on === '') return true;
  const [path, wanted] = key.depends_on.split('=');
  const parent = (contract.keys ?? []).find((each) => each.path === path);
  if (parent === undefined) return true;
  if (!isActive(parent, contract, answers)) return false;

  const shown = shownOf(parent, answers);
  if (wanted !== undefined) return shown === wanted;
  if (typeof shown === 'boolean') return shown;

  return shown.trim() !== '';
};

/** The path of the key that turns this one on, without a value. */
export const parentOf = (key: SetupKey): string | undefined =>
  key.depends_on === undefined || key.depends_on === ''
    ? undefined
    : key.depends_on.split('=')[0];

/** Why an answer cannot be sent. */
export type AnswerError = 'integer' | 'duration' | 'choice';

const DURATION = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/;
const INTEGER = /^-?\d+$/;

/** Checks one answer against its key, as the server will. */
export const errorOf = (
  key: SetupKey,
  answer: Answer,
): AnswerError | undefined => {
  if (typeof answer !== 'string' || answer === '') return undefined;
  if (key.kind === 'number' && !INTEGER.test(answer.trim())) return 'integer';
  if (key.kind === 'duration' && !DURATION.test(answer.trim())) {
    return 'duration';
  }
  if (
    key.choices !== null &&
    key.choices.length > 0 &&
    !key.choices.includes(answer)
  ) {
    return 'choice';
  }

  return undefined;
};

/** The value the wire carries for one answer. */
const wireOf = (key: SetupKey, answer: Answer): unknown => {
  if (typeof answer === 'boolean') return answer;
  if (key.kind === 'number') return Number(answer.trim());
  if (key.kind === 'text_list') {
    return answer
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== '');
  }

  return answer.trim();
};

/** One key whose answer differs from where it started. */
export interface Change {
  key: SetupKey;
  answer: Answer;
}

/**
 * The answers that change something: a key the operator's file does not set,
 * answered with a value that is not where the key started. These, and only
 * these, are written (ADR-20261015-ejz rule 29).
 */
export const changesOf = (
  contract: SetupConfiguration,
  answers: Readonly<Record<string, Answer>>,
  mode?: SetupMode,
): Change[] =>
  (contract.keys ?? []).flatMap((key) => {
    const answer = answers[key.path];
    if (key.locked || answer === undefined) return [];
    if (IDENTITY_KEYS.includes(key.path) || idle(key, mode)) return [];
    if (!isActive(key, contract, answers)) return [];
    if (answer === startOf(key)) return [];
    if (
      typeof answer === 'string' &&
      answer.trim() === '' &&
      startOf(key) === ''
    ) {
      return [];
    }

    return [{ key, answer }];
  });

/** A set of keys that only work together. */
export type KeyGroup = 'identity' | 'encryption';

/**
 * The group a key holds together, or undefined for a key that stands alone.
 * The redirect address stands alone: the wizard fills it in itself, and the
 * file's address works with any provider. A secret stands alone too, because
 * the server never shows its value, so the wizard cannot tell it differs.
 */
const groupOf = (key: SetupKey): KeyGroup | undefined => {
  if (key.secret || key.path === 'core.auth.redirect_uri') return undefined;
  if (IDENTITY_KEYS.includes(key.path)) return 'identity';
  if (key.path.startsWith('core.api.tls.')) return 'encryption';

  return undefined;
};

/** What the operator's file does to the configuration the wizard would send. */
export interface Settled {
  /** What the confirm sends. */
  configuration: Record<string, unknown>;
  /** Keys the file sets to a value other than the wizard's, which it keeps. */
  kept: SetupKey[];
  /**
   * Groups the wizard cannot finish: the file sets one of their keys to
   * another value, and half of a group from the file and half from the wizard
   * does not work. The person changes the step or the file first.
   */
  blocked: KeyGroup[];
}

/** Whether a value the wizard would send is the one the file sets. */
const sameValue = (key: SetupKey, value: unknown): boolean => {
  if (key.secret || key.value === undefined) return false;
  if (JSON.stringify(key.value) === JSON.stringify(value)) return true;

  return wireOf(key, startOf(key)) === value;
};

/**
 * The configuration as the operator's file leaves it. The server refuses the
 * whole confirm for one key the file sets (ADR-20261015-ejz rule 29), and the steps
 * that write a key themselves, such as the printers' address, do not read the
 * contract. A key the file sets to the same value is left out and nobody is
 * told. A key the file sets to another value is left out and named, so the
 * summary can say the file wins. Sign-in and certificate keys only work
 * together, so when the file sets one of them to another value the group is
 * blocked, and the summary does not confirm until the person settles it.
 */
export const settleLocked = (
  contract: SetupConfiguration,
  configuration: Readonly<Record<string, unknown>>,
): Settled => {
  const keys = new Map((contract.keys ?? []).map((key) => [key.path, key]));
  const kept: SetupKey[] = [];
  const blocked = new Set<KeyGroup>();

  for (const [path, value] of Object.entries(configuration)) {
    const key = keys.get(path);
    if (key === undefined || !key.locked || sameValue(key, value)) continue;
    kept.push(key);
    const group = groupOf(key);
    if (group !== undefined) blocked.add(group);
  }

  return {
    configuration: Object.fromEntries(
      Object.entries(configuration).filter(
        ([path]) => keys.get(path)?.locked !== true,
      ),
    ),
    kept,
    blocked: [...blocked],
  };
};

/** The `configuration` member of the confirm. */
export const configurationOf = (
  contract: SetupConfiguration,
  answers: Readonly<Record<string, Answer>>,
  mode?: SetupMode,
): Record<string, unknown> =>
  Object.fromEntries(
    changesOf(contract, answers, mode).map(({ key, answer }) => [
      key.path,
      wireOf(key, answer),
    ]),
  );

/**
 * The origin the server answers on after the restart, when the answers move
 * it. A host left empty or written as every interface is the one this page
 * was opened on.
 */
export const originAfter = (
  contract: SetupConfiguration,
  answers: Readonly<Record<string, Answer>>,
  current: URL,
): string => {
  const find = (path: string): SetupKey | undefined =>
    (contract.keys ?? []).find((key) => key.path === path);
  const addrKey = find('core.api.addr');
  const certificateKey = find('core.api.tls.certificate');
  const addr = addrKey === undefined ? '' : String(shownOf(addrKey, answers));
  const certificate =
    certificateKey === undefined
      ? ''
      : String(shownOf(certificateKey, answers));

  const colon = addr.lastIndexOf(':');
  const port = colon === -1 ? '' : addr.slice(colon + 1);
  const host = colon === -1 ? addr : addr.slice(0, colon);
  const everywhere = ['', '0.0.0.0', '::', '[::]'];
  const hostname = everywhere.includes(host) ? current.hostname : host;
  const scheme = certificate.trim() === '' ? 'http:' : 'https:';

  const next = new URL(current.href);
  next.protocol = scheme;
  next.hostname = hostname;
  next.port = port;

  return next.origin;
};

/**
 * The origin the page goes to after the restart, or undefined when the
 * answers leave the listener and its certificate as they are. A browser that
 * reached the server through a published container port or a proxy sees
 * another port than the server listens on. That server does not move, so the
 * page stays where it is.
 */
export const movedOrigin = (
  contract: SetupConfiguration,
  answers: Readonly<Record<string, Answer>>,
  current: URL,
): string | undefined => {
  const next = originAfter(contract, answers, current);
  if (next === originAfter(contract, {}, current)) return undefined;

  return next === current.origin ? undefined : next;
};
