import type { ResourceInstance } from '../../data';

export type ResourceOption = NonNullable<
  NonNullable<ResourceInstance['module']>['options']
>[number];

export type Control = 'text' | 'number' | 'switch' | 'secret' | 'json';

// The API types an option's type as a plain string, so an unknown one falls back to text.
const CONTROL: Readonly<Record<string, Control>> = {
  string: 'text',
  url: 'text',
  duration: 'text',
  int: 'number',
  number: 'number',
  bool: 'switch',
  secret: 'secret',
  list: 'json',
  object: 'json',
};

export const controlOf = (option: ResourceOption): Control =>
  CONTROL[option.type] ?? 'text';

/**
 * What the server writes in place of a stored secret, and reads back as
 * "keep the stored value" (internal/app/resource/secrets.go). Sent for a key
 * with nothing stored, it is refused.
 */
export const SECRET_KEPT = 'xxxxx';

export type Draft = Readonly<Record<string, string | boolean>>;

/** The form's starting values; a stored secret starts empty and is kept. */
export const draftOf = (
  options: readonly ResourceOption[],
  config: Readonly<Record<string, unknown>>,
): { draft: Draft; stored: ReadonlySet<string> } => {
  const draft: Record<string, string | boolean> = {};
  const stored = new Set<string>();

  for (const option of options) {
    const value = config[option.key];
    const control = controlOf(option);

    if (control === 'secret') {
      if (value === SECRET_KEPT) stored.add(option.key);
      draft[option.key] = '';
    } else if (control === 'switch') {
      draft[option.key] =
        typeof value === 'boolean' ? value : option.default === 'true';
    } else if (value === undefined || value === null) {
      draft[option.key] = '';
    } else {
      draft[option.key] =
        control !== 'json' && typeof value === 'string'
          ? value
          : JSON.stringify(value);
    }
  }

  return { draft, stored };
};

export type FieldError =
  'required' | 'reserved' | 'integer' | 'number' | 'url' | 'list' | 'object';

type Parsed = { value: unknown } | { error: FieldError };

const parseJSON = (text: string): unknown => {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
};

const parse = (option: ResourceOption, text: string): Parsed => {
  switch (option.type) {
    case 'int':
      return /^-?\d+$/.test(text)
        ? { value: Number(text) }
        : { error: 'integer' };
    case 'number':
      return Number.isFinite(Number(text))
        ? { value: Number(text) }
        : { error: 'number' };
    case 'url':
      return URL.canParse(text) ? { value: text } : { error: 'url' };
    case 'list': {
      const value = parseJSON(text);
      return Array.isArray(value) ? { value } : { error: 'list' };
    }
    case 'object': {
      const value = parseJSON(text);
      return typeof value === 'object' &&
        value !== null &&
        !Array.isArray(value)
        ? { value }
        : { error: 'object' };
    }
    default:
      return { value: text };
  }
};

/**
 * The config a draft saves, or what is wrong with each field. The server
 * checks a value's type only when the module starts, so a wrong one is
 * refused here, at its field. An empty field, and a switch left at its
 * default with nothing stored, is left out, so the module reads its default.
 * A key of `saved` that no option declares was written by hand into the
 * configuration, and is kept as it was.
 */
export const configOf = (
  options: readonly ResourceOption[],
  draft: Draft,
  kept: ReadonlySet<string>,
  saved: Readonly<Record<string, unknown>> = {},
): {
  config: Record<string, unknown>;
  errors: Readonly<Record<string, FieldError>>;
} => {
  const declared = new Set(options.map((option) => option.key));
  const config: Record<string, unknown> = Object.fromEntries(
    Object.entries(saved).filter(([key]) => !declared.has(key)),
  );
  const errors: Record<string, FieldError> = {};

  for (const option of options) {
    const entry = draft[option.key];

    if (typeof entry === 'boolean') {
      const atDefault =
        option.default !== undefined && entry === (option.default === 'true');
      if (!atDefault || saved[option.key] !== undefined) {
        config[option.key] = entry;
      }
      continue;
    }

    // A secret is sent as typed: spaces may be part of it.
    if (option.type === 'secret') {
      const secret = entry ?? '';
      if (secret === '') {
        if (kept.has(option.key)) config[option.key] = SECRET_KEPT;
        else if (option.required) errors[option.key] = 'required';
      } else if (secret === SECRET_KEPT) {
        errors[option.key] = 'reserved';
      } else {
        config[option.key] = secret;
      }
      continue;
    }

    const text = (entry ?? '').trim();

    if (text === '') {
      if (option.required && option.default === undefined)
        errors[option.key] = 'required';
      continue;
    }

    const parsed = parse(option, text);
    if ('error' in parsed) errors[option.key] = parsed.error;
    else config[option.key] = parsed.value;
  }

  return { config, errors };
};
