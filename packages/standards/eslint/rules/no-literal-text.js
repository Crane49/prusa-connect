// The gate behind `frontend-i18n.md` rule 1: no English string is written
// into a component.
//
// It reads the tree `svelte-eslint-parser` builds, so it sees the two places a
// person actually types a sentence: the text between two tags, and the four
// attributes a reader hears rather than sees. Everything else in a template is
// an expression, and an expression is `t('key')` or it is not a text.
//
// There is no shipped rule for this. `eslint-plugin-svelte` has `no-raw-text`
// in the Svelte 3 rule set only, and it does not read `title`, `aria-label`,
// `placeholder` or `alt`, which is half of what has to be caught.

/** The attributes a person reads or hears, which therefore carry text. */
const TEXT_ATTRIBUTES = new Set(['title', 'aria-label', 'placeholder', 'alt']);

/** Two letters in a row is a word. One letter, a digit or a symbol is not. */
const WORD = /\p{L}\p{L}/u;

/**
 * What is text in English but is the same text in every language: a unit, a
 * material, a brand. Compared against the trimmed value, so `PLA` passes and
 * `PLA spool` does not.
 */
const ALLOWED = [
  'PRUSA',
  'Prusa',
  'Prusa Connect',
  'PLA',
  'PETG',
  'ASA',
  'PC',
  'mm',
  'mm/s',
  'kB',
  'MB',
  'GB',
  'ms',
];

const MESSAGE =
  'A text written in English. Move it to locales/en.json and render it with t() (frontend-i18n.md rule 1).';

/**
 * Whether an ancestor is `<script>` or `<style>`, whose text is code.
 *
 * @param {unknown} node
 * @returns {boolean}
 */
const inCode = (node) => {
  let at = node;

  while (typeof at === 'object' && at !== null) {
    const held = /** @type {{ type?: string, parent?: unknown }} */ (at);
    if (
      held.type === 'SvelteScriptElement' ||
      held.type === 'SvelteStyleElement'
    ) {
      return true;
    }
    at = held.parent;
  }

  return false;
};

/** @param {unknown} context */
const create = (context) => {
  const held =
    /** @type {{
     *   options: { allow?: string[] }[],
     *   report: (given: { node: unknown, messageId: string }) => void,
     * }} */ (context);
  const allowed = new Set([...ALLOWED, ...(held.options[0]?.allow ?? [])]);

  /**
   * @param {string} value
   * @returns {boolean}
   */
  const isText = (value) => {
    const trimmed = value.trim();

    return trimmed !== '' && WORD.test(trimmed) && !allowed.has(trimmed);
  };

  return {
    /** @param {{ value: string }} node */
    SvelteText(node) {
      if (inCode(node) || !isText(node.value)) return;

      held.report({ node, messageId: 'literal' });
    },

    /**
     * @param {{
     *   key: { name: string },
     *   value: { type: string, value?: string }[],
     * }} node
     */
    SvelteAttribute(node) {
      if (!TEXT_ATTRIBUTES.has(node.key.name)) return;
      if (!node.value.every((part) => part.type === 'SvelteLiteral')) return;

      const value = node.value.map((part) => part.value ?? '').join('');
      if (!isText(value)) return;

      held.report({ node, messageId: 'literal' });
    },
  };
};

const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'refuse an English string in Svelte markup text and in the attributes a person reads',
    },
    schema: [
      {
        type: 'object',
        properties: { allow: { type: 'array', items: { type: 'string' } } },
        additionalProperties: false,
      },
    ],
    messages: { literal: MESSAGE },
  },
  create,
};

// The visitor names are the Svelte parser's, and `RuleListener` has an index
// signature for the ESTree ones alone, so the whole rule is cast once here
// rather than each visitor being bent to a type it does not have.
export const noLiteralText = /** @type {import('eslint').Rule.RuleModule} */ (
  /** @type {unknown} */ (rule)
);
