// The application's logger (.agents/rules/frontend-logging.md). Besides the
// console, it keeps the newest entries in memory for a diagnostics bundle.
// A downloaded bundle is scrubbed again by the server, but "Copy browser
// details" puts the buffer on the clipboard as this pass leaves it, so the
// patterns here follow internal/app/diagnostics/scrub.go.

export type Level = 'error' | 'warn' | 'info' | 'debug' | 'trace';
export type Fields = Readonly<Record<string, unknown>>;

export interface Entry {
  readonly time: string;
  readonly level: Level;
  readonly scope: string;
  /** The router template of the screen the entry was written on. */
  readonly route?: string;
  readonly message: string;
  readonly fields: Fields;
}

export interface Logger {
  error(message: string, fields?: Fields | (() => Fields)): void;
  warn(message: string, fields?: Fields | (() => Fields)): void;
  info(message: string, fields?: Fields | (() => Fields)): void;
  debug(message: string, fields?: Fields | (() => Fields)): void;
  trace(message: string, fields?: Fields | (() => Fields)): void;
  scope(name: string): Logger;
}

const RANK: Readonly<Record<Level, number>> = {
  error: 0,
  warn: 1,
  info: 2,
  debug: 3,
  trace: 4,
};
const CAPACITY = 200;
const SENSITIVE = [
  'password',
  'token',
  'secret',
  'cookie',
  'authorization',
  'email',
  'username',
  'subject',
  'gcode',
  'name',
  'file',
  'path',
];
type Mask = string | ((match: string) => string);

// A candidate is an IPv6 address only when the URL parser takes it, because
// the pattern also matches a clock time such as 12:34:56.
const ipv6 = (candidate: string): string => {
  const host = candidate.replace(/^\[/u, '').replace(/\].*$/u, '');
  try {
    new URL(`http://[${host}]`);
  } catch {
    return candidate;
  }

  return '<ip>';
};

// A Go source position such as main.go:42 names no machine, and a port
// names nothing of the operator's.
const host = (candidate: string): string => {
  const [name, port] = candidate.split(':');
  if (port === undefined) return '<host>';
  return name?.endsWith('.go') ? candidate : `<host>:${port}`;
};

// An API route names no file of the operator's.
const path = (candidate: string): string =>
  candidate.startsWith('/api/') ? candidate : '<path>';

// A long run with both digits and letters is an opaque token. A UUID, a trace
// id and a commit revision are what a report is correlated by, and stay.
const long = (match: string): string => {
  const token = match.replace(/^revision=/u, '');
  if (
    /^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$/iu.test(token) ||
    /^[\da-f]{32}$/iu.test(token) ||
    (token !== match && /^[\da-f]{40}$/iu.test(token)) ||
    !/\d/u.test(token) ||
    !/[a-z]/iu.test(token)
  ) {
    return match;
  }

  return '<redacted>';
};

// The order matters: a URL is reduced before its parts could match anything
// narrower. The browser holds no known secret, so the sweep scrub.go runs
// first has no counterpart here.
const PATTERNS: readonly (readonly [RegExp, Mask])[] = [
  [/\b[a-z][a-z0-9+.-]*:\/\/[^\s"'<>]+/giu, '<url>'],
  [/\beyJ[\w-]*\.[\w-]+\.[\w-]*/gu, '<jwt>'],
  [/\bbearer\s+[\w.~+/=-]+/giu, '<bearer>'],
  [
    /(\bauthorization"?\s*:\s*["[]{0,2})(?:basic|token|digest)\s+[^\r\n"\]]+/giu,
    '$1<credentials>',
  ],
  [/\b((?:set-)?cookie"?\s*:\s*["[]{0,2})[^\r\n"\]]+/giu, '$1<redacted>'],
  [
    /([\w-]*(?:password|passwd|pwd|token|secret|api[_-]?key)"?)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&,;]+)/giu,
    '$1$2<redacted>',
  ],
  [/[\w.%+-]+@[\w.-]+\.[a-z]{2,}/giu, '<email>'],
  [
    /\b((?:user|role|username|login)(?:\s+|\s*[=:]\s*))(?:"[^"]*"|'[^']*')/giu,
    '$1<user>',
  ],
  [/\b[\da-f]{2}(?:[:-][\da-f]{2}){5}\b/giu, '<mac>'],
  [/\blookup\s+[a-z\d-]+(?:\.[a-z\d-]+)*/giu, 'lookup <host>'],
  [/\b(?:\d{1,3}\.){3}\d{1,3}(?::\d{1,5})?\b/gu, '<ip>'],
  [/\[?[\da-f]{0,4}(?::[\da-f]{0,4}){2,7}\]?(?::\d{1,5})?/giu, ipv6],
  [
    /\b[a-z\d-]+(?:\.[a-z\d-]+)+:\d{1,5}\b|\b[a-z\d-]+(?:\.[a-z\d-]+)*\.(?:local|lan|home|internal|localdomain|home\.arpa)\b/giu,
    host,
  ],
  // A home directory is one segment, because a user name may hold a space.
  [
    /\/(?:Users|home)\/[^/\n"':]+(?:\/[^\s"':/\\]+)+|[a-z]:\\Users\\[^\\\n"':]+(?:\\[^\s"':/\\]+)+/giu,
    '<path>',
  ],
  [
    /(?:[a-z\d-]+(?:\.[a-z\d-]+)*\.(?:com|org|net|io|in|dev))?(?:[a-z]:\\|\/)(?:[^\s"':/\\]+[/\\])+[^\s"':/\\]+/giu,
    path,
  ],
  [/(?:revision=)?\b[\w+-]{32,}={0,2}/gu, long],
];

const ring: Entry[] = [];
let route: string | undefined;
// ponytail: the level is fixed per build; read static/config.js when an operator needs to change it at runtime.
const threshold: Level = import.meta.env.DEV ? 'debug' : 'warn';

const scrub = (text: string): string =>
  PATTERNS.reduce(
    (out, [pattern, mask]) =>
      typeof mask === 'string'
        ? out.replace(pattern, mask)
        : out.replace(pattern, mask),
    text,
  );

// A copy all the way down, so nothing in the ring is shared with the caller.
// An Error is kept as its text. Any other object that is not plain data is
// not something a log field was meant to carry, and is dropped.
const clean = (value: unknown): unknown => {
  if (typeof value === 'string') return scrub(value);
  if (Array.isArray(value)) return value.map(clean);
  if (value instanceof Error) return scrub(`${value.name}: ${value.message}`);
  if (typeof value === 'object' && value !== null) {
    const proto: unknown = Object.getPrototypeOf(value);

    return proto === Object.prototype || proto === null
      ? redact(value as Fields)
      : undefined;
  }

  return value;
};

export const redact = (fields: Fields): Fields =>
  Object.fromEntries(
    Object.entries(fields)
      .filter(
        ([key]) => !SENSITIVE.some((word) => key.toLowerCase().includes(word)),
      )
      .map(([key, value]) => [key, clean(value)]),
  );

const write = (
  level: Level,
  scope: string,
  message: string,
  fields: Fields | (() => Fields) | undefined,
): void => {
  const keep = RANK[level] <= RANK.info;
  const show = RANK[level] <= RANK[threshold];
  if (!keep && !show) return;

  const entry: Entry = {
    time: new Date().toISOString(),
    level,
    scope,
    ...(route === undefined ? {} : { route }),
    message: scrub(message),
    fields: redact((typeof fields === 'function' ? fields() : fields) ?? {}),
  };

  if (keep) {
    ring.push(entry);
    if (ring.length > CAPACITY) ring.shift();
  }

  if (show) {
    console[level === 'trace' ? 'debug' : level](
      `[${scope}] ${entry.message}`,
      entry.fields,
    );
  }
};

const scoped = (scope: string): Logger => ({
  error: (message, fields) => {
    write('error', scope, message, fields);
  },
  warn: (message, fields) => {
    write('warn', scope, message, fields);
  },
  info: (message, fields) => {
    write('info', scope, message, fields);
  },
  debug: (message, fields) => {
    write('debug', scope, message, fields);
  },
  trace: (message, fields) => {
    write('trace', scope, message, fields);
  },
  scope: (name) => scoped(name),
});

export const log: Logger = scoped('app');

const navLog = log.scope('nav');

/**
 * Records a move to another screen, so the ring holds the path the person
 * took. It takes the router's template (`/printers/:id`), never the address,
 * because an address can carry a file or folder name.
 */
export const navigate = (pattern: string | undefined): void => {
  route = pattern;
  navLog.info('navigate', { route: pattern });
};

export const entries = (): readonly Entry[] => [...ring];
