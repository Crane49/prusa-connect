import type { Snippet } from 'svelte';
import type { ClassValue, HTMLButtonAttributes } from 'svelte/elements';

/**
 * The five looks. `default` is the brand fill, `secondary` the quiet fill,
 * `ghost` a button with no edge at rest, and `destructive` an outline that
 * fills only when it is touched. `link` is text with no box that underlines on
 * hover, and it inherits the surrounding font size.
 */
export type ButtonVariant =
  'default' | 'secondary' | 'ghost' | 'destructive' | 'link';

/** The three sizes that hold text. */
export type ButtonTextSize = 'sm' | 'md' | 'lg';

/** A square size for each text size. A square button holds one glyph. */
export type ButtonIconSize = 'icon-sm' | 'icon-md' | 'icon-lg';

/**
 * The size of a `link` button that carries an icon: it lays out as a flex row
 * with a gap, and wraps its text.
 */
export type ButtonLinkSize = 'inline-icon';

export type ButtonSize = ButtonTextSize | ButtonIconSize | ButtonLinkSize;

interface ButtonBaseProps extends Omit<
  HTMLButtonAttributes,
  'class' | 'children'
> {
  variant?: ButtonVariant;
  class?: ClassValue | undefined;
  children?: Snippet;
  /**
   * The act the button starts is under way. A spinner leads the content, the
   * button is `aria-busy` and cannot be pressed. Not drawn by a `child`.
   */
  pending?: boolean | undefined;
  /**
   * Draw the button as another element, an anchor or a menu item. The snippet
   * receives the attributes the `<button>` would have had and puts them where
   * it wants them. Nothing is merged for it: an attribute the snippet does not
   * spread is lost.
   */
  child?: Snippet<[{ props: Record<string, unknown> }]>;
}

/**
 * A square button shows no text, so it requires `label`. The label is the
 * button's accessible name and the text of the tooltip it shows on hover and
 * on keyboard focus. A text button has its name in its text and takes no
 * label.
 */
export type ButtonProps = ButtonBaseProps &
  (
    | { size?: ButtonTextSize | ButtonLinkSize; label?: never }
    | { size: ButtonIconSize; label: string }
  );
