// The skin the page is drawn in, as an attribute on the root element.
//
// A skin swaps token values inside `tokens.css`: `:root[data-skin='slicer']`
// and `:root[data-skin='prusapro']` each hold a block of overrides, with a
// second block under `.dark`. The two-attribute selector scores 0-2-0 against
// `.dark`'s 0-1-0, so a skin wins in both themes. `prusa` is the default and
// matches no selector.
//
// The theme is one stylesheet, so the colour is already there at the first
// paint and no request can arrive late and repaint a page somebody is reading.

/** Every skin `tokens.css` declares, the default first. */
export const SKIN_NAMES = ['prusa', 'slicer', 'prusapro'] as const;

export type Skin = (typeof SKIN_NAMES)[number];

/** The skin that carries no attribute: the values `tokens.css` declares. */
export const DEFAULT_SKIN: Skin = 'prusa';

const isSkin = (value: string): value is Skin =>
  (SKIN_NAMES as readonly string[]).includes(value);

/**
 * Draws the page in the named skin.
 *
 * A name this theme has no block for falls back to the default, and says
 * nothing to the person: the colour of a button is not theirs to repair.
 */
export const applySkin = (name: string): void => {
  // Under SSR there is no document, and a page rendered there carries the
  // default skin until this runs in the browser.
  if (typeof document === 'undefined') return;
  document.documentElement.dataset.skin = isSkin(name) ? name : DEFAULT_SKIN;
};
