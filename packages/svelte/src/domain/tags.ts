// This decides when two tags are the same tag. The server compares tags this
// way, and a screen that groups or filters by tag compares them the same way.

// The joiner U+200D, the non-joiner U+200C and the presentation selectors
// U+FE0E and U+FE0F. A person cannot tell two tags apart by them.
const INVISIBLE = /\u200C|\u200D|\uFE0E|\uFE0F/gu;

const SPACES = /\s+/u;

// A fold that turns one character into two is not taken, so this counts code
// points rather than UTF-16 units.
const single = (text: string): boolean => Array.from(text).length === 1;

/**
 * One character folded to the letter it shares with its other cases. The
 * server folds one letter to one letter, so the sharp s stays the sharp s,
 * the Greek final sigma meets the other sigma, and the Kelvin sign meets k.
 */
const foldOne = (char: string): string => {
  const upper = char.toUpperCase();
  const base = single(upper) ? upper : char;
  const lower = base.toLowerCase();

  return single(lower) ? lower : base;
};

/**
 * The text two tags share when they are the same tag. The ends are trimmed,
 * a run of spaces becomes one space, the invisible joiners and selectors are
 * removed, and each character is folded on its own.
 */
export const tagKey = (tag: string): string =>
  Array.from(tag.trim().split(SPACES).join(' ').replace(INVISIBLE, ''))
    .map(foldOne)
    .join('');

/** Whether two tags are the same tag, as the server decides it. */
export const sameTag = (a: string, b: string): boolean =>
  tagKey(a) === tagKey(b);
