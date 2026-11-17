import type { Snippet } from 'svelte';
import type { ClassValue, HTMLAttributes } from 'svelte/elements';

export type TileSize = 'sm' | 'md';

/** What a `child` snippet spreads onto its own press element. */
export interface TilePressProps {
  /** What the frame's `has-[…]` focus selectors find. */
  'data-slot': 'tile-press';
  class: string;
  'aria-label': string | undefined;
  'aria-disabled': true | undefined;
}

export interface TileProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children' | 'onclick'
> {
  /** The tile is a link. */
  href?: string | undefined;
  /** The tile is a button. */
  onclick?: ((event: MouseEvent) => void) | undefined;
  /** Sets `aria-pressed` on the button and draws the selected look. */
  pressed?: boolean | undefined;
  /** The selected look alone, for a tile picked with a checkbox in `top`. */
  selected?: boolean | undefined;
  disabled?: boolean | undefined;
  /** A dashed destructive border: the thing the tile stands for is switched off. */
  off?: boolean | undefined;
  size?: TileSize | undefined;
  /** The press element's accessible name, when its content does not say it. */
  label?: string | undefined;
  /** Controls drawn above the press element's hit area: a checkbox, row actions. */
  top?: Snippet | undefined;
  /**
   * A press element of the caller's own, such as a link that navigates on the
   * client. It spreads `props` onto that element and renders the content inside.
   */
  child?: Snippet<[{ props: TilePressProps }]> | undefined;
  /** The content. Its layout, row or column, is the caller's. */
  children?: Snippet | undefined;
  class?: ClassValue | undefined;
}
