// How one message becomes one string: named parameters, and plurals through
// `Intl.PluralRules`.
//
// The syntax is the part of ICU MessageFormat this library needs and no more.
// `{name}` is a named parameter. `{count, plural, one {…} other {…}}` picks a
// branch by the plural category the locale gives, and `#` inside a branch is
// the number itself, formatted for that locale. Nothing here builds a plural
// by comparing a count to one: Czech has four categories and Polish has
// three, so a comparison against one is wrong in both.

/** What a message may be given. A value is a string or a number, never markup. */
export type MessageParams = Readonly<Record<string, string | number>>;

/** The whole of one language's messages for one namespace, keys flattened. */
export type Messages = Readonly<Record<string, string>>;

const NUMBERS = new Map<string, Intl.NumberFormat>();

const plainNumber = (locale: string): Intl.NumberFormat => {
  const held = NUMBERS.get(locale);
  if (held) return held;

  const made = new Intl.NumberFormat(locale);
  NUMBERS.set(locale, made);

  return made;
};

const PLURALS = new Map<string, Intl.PluralRules>();

const pluralRules = (locale: string): Intl.PluralRules => {
  const held = PLURALS.get(locale);
  if (held) return held;

  const made = new Intl.PluralRules(locale);
  PLURALS.set(locale, made);

  return made;
};

/**
 * The index of the `}` that closes the `{` at `open`, or -1 when the message
 * has none. A plural branch carries braces of its own, so the depth is
 * counted rather than the first `}` taken.
 */
const closingBrace = (message: string, open: number): number => {
  let depth = 0;

  for (let at = open; at < message.length; at += 1) {
    const character = message[at];
    if (character === '{') depth += 1;
    if (character === '}') {
      depth -= 1;
      if (depth === 0) return at;
    }
  }

  return -1;
};

/**
 * The branches of a plural, as `one {…}` pairs. A branch named `=0` is an
 * exact count and wins over the category.
 */
const branchesOf = (body: string): Map<string, string> => {
  const found = new Map<string, string>();
  let at = 0;

  while (at < body.length) {
    const open = body.indexOf('{', at);
    if (open === -1) break;

    const close = closingBrace(body, open);
    if (close === -1) break;

    const name = body.slice(at, open).trim();
    if (name !== '') found.set(name, body.slice(open + 1, close));
    at = close + 1;
  }

  return found;
};

/**
 * One `{…}` rendered. `raw` is what stood between the braces, and it is
 * returned with its braces when the message asks for a parameter the caller
 * did not pass: a visible `{name}` is a fault somebody fixes, and an empty
 * space is one nobody sees.
 */
const placeholder = (
  raw: string,
  params: MessageParams | undefined,
  locale: string,
): string => {
  const comma = raw.indexOf(',');

  if (comma === -1) {
    const name = raw.trim();
    const given = params?.[name];

    if (given === undefined) return `{${raw}}`;

    return typeof given === 'number'
      ? plainNumber(locale).format(given)
      : given;
  }

  const name = raw.slice(0, comma).trim();
  const rest = raw.slice(comma + 1).trimStart();
  const given = params?.[name];

  if (!rest.startsWith('plural') || typeof given !== 'number') {
    return `{${raw}}`;
  }

  const branches = branchesOf(rest.slice('plural'.length).replace(',', ''));
  const chosen =
    branches.get(`=${String(given)}`) ??
    branches.get(pluralRules(locale).select(given)) ??
    branches.get('other');

  if (chosen === undefined) return `{${raw}}`;

  return format(
    chosen.split('#').join(plainNumber(locale).format(given)),
    params,
    locale,
  );
};

/**
 * A message with its parameters filled in, for one locale. A message with no
 * `{` is returned as it was written, which is most of them.
 */
export const format = (
  message: string,
  params: MessageParams | undefined,
  locale: string,
): string => {
  let out = '';
  let at = 0;

  while (at < message.length) {
    const open = message.indexOf('{', at);
    if (open === -1) {
      out += message.slice(at);
      break;
    }

    out += message.slice(at, open);

    const close = closingBrace(message, open);
    if (close === -1) {
      out += message.slice(open);
      break;
    }

    out += placeholder(message.slice(open + 1, close), params, locale);
    at = close + 1;
  }

  return out;
};
