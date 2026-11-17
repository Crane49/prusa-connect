import type { ClassValue } from 'svelte/elements';

/**
 * The drawings this library names. They are quiet line art, drawn from the
 * printer silhouettes, and a person's own drawing can replace any of the
 * files without a change here.
 */
export const ART_NAMES = [
  'empty-fleet',
  'fractal',
  'shelf',
  'camera-grid',
] as const;

export type ArtName = (typeof ART_NAMES)[number];

/**
 * How the drawing sits in the box it is given. `contain` keeps the whole
 * drawing, `cover` fills the box and lets the edges run out of it, and `tile`
 * repeats the drawing at its own size, which is what a bed grid does.
 */
export type ArtFit = 'contain' | 'cover' | 'tile';

export interface ArtProps {
  name: ArtName;
  /**
   * Where the artwork is served from. An application serves the folder at
   * this path; a page under a different prefix passes its own.
   */
  base?: string;
  /** What a screen reader says instead of the drawing. */
  alt?: string;
  /**
   * Texture rather than a picture: the shelf under a silhouette and the grid
   * behind a camera frame say nothing a person needs read out, so they are
   * hidden from a screen reader.
   */
  decorative?: boolean;
  fit?: ArtFit;
  class?: ClassValue | undefined;
}
