// A fault, as a person reads it.
//
// Every fault the core answers carries a stable code, and the code is the key
// a translation is written against. `docs/errors/error-codes.json` is the
// registry, `scripts/emit-catalogues.mjs` turns it into `error.<code>` lines
// in the English catalogue, and a translator works from that one list.
//
// The server's own English message is kept beside the translation and never
// thrown away. It carries the detail the registry line cannot: which object,
// which field, which printer. So the title is translated and the detail is
// quoted, and an operator reading a Czech screen can still paste the English
// line into a bug report.
import { CORE_NAMESPACE, locale } from './locale.svelte';
import type { MessageParams } from './messages';
import { format } from './messages';

/** What a screen draws for a fault. */
export interface FaultText {
  /** The registry code, when the answer carried one. Shown to an operator. */
  code: string | undefined;
  /** The translated line, or the server's English one when nothing is translated. */
  title: string;
  /** The server's English message, when it says more than the title. */
  detail: string | undefined;
  /**
   * What the reader can do about it, from the registry's own action line.
   *
   * Optional, and not merely undefined: a module builds this shape itself for
   * a fault of its own, and a module with no action line writes no field.
   */
  action?: string | undefined;
  /** The request's id in the core's log, for an operator to quote. Optional for the same reason as `action`. */
  traceId?: string | undefined;
}

interface FaultShape {
  code?: unknown;
  message?: unknown;
  params?: unknown;
  traceId?: unknown;
}

const textOf = (value: unknown): string | undefined =>
  typeof value === 'string' && value.trim() !== '' ? value : undefined;

const paramsOf = (value: unknown): MessageParams | undefined => {
  if (typeof value !== 'object' || value === null) return undefined;

  const kept: Record<string, string | number> = {};
  for (const [name, given] of Object.entries(value)) {
    if (typeof given === 'string' || typeof given === 'number') {
      kept[name] = given;
    }
  }

  return kept;
};

/**
 * The server's line without the code the SDK writes in front of it. An
 * `ApiError` carries `"<code>: <message>"`, so the comparison below would
 * never find the two equal and every screen would draw the same sentence
 * twice, the second time with the code in it.
 */
const unprefixed = (
  english: string | undefined,
  code: string | undefined,
): string | undefined => {
  if (english === undefined || code === undefined) return english;

  const lead = `${code}: `;

  return english.startsWith(lead)
    ? textOf(english.slice(lead.length))
    : english;
};

/**
 * The three parts of a fault: its code, the line to show, and the English
 * detail to show under it.
 *
 * It reads an `ApiError` from `@prusa/connect` by shape rather than by
 * class, so a fault a module's own client threw is read the same way. A
 * thrown value with no code at all is the network case, and it gets the one
 * key this library keeps for it.
 */
export const describeFault = (thrown: unknown): FaultText => {
  const fault: FaultShape =
    typeof thrown === 'object' && thrown !== null ? thrown : {};

  const code = textOf(fault.code);
  const english = unprefixed(textOf(fault.message) ?? textOf(thrown), code);
  const line =
    code === undefined
      ? undefined
      : locale.message(CORE_NAMESPACE, `error.${code}`);

  const title =
    line === undefined
      ? (english ?? locale.message(CORE_NAMESPACE, 'error.unknown') ?? '')
      : format(line, paramsOf(fault.params), locale.current);

  const action =
    code === undefined
      ? undefined
      : locale.message(CORE_NAMESPACE, `error.${code}.action`);

  return {
    code,
    title,
    detail: english === undefined || english === title ? undefined : english,
    action:
      action === undefined
        ? undefined
        : format(action, paramsOf(fault.params), locale.current),
    traceId: textOf(fault.traceId),
  };
};
