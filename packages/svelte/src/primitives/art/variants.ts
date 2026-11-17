import type { ArtName } from './types';

/** The drawing's file, under the component's `base`. */
export const ART_FILE: Record<ArtName, string> = {
  'empty-fleet': 'empty-fleet.svg',
  fractal: 'fractal.svg',
  shelf: 'shelf.svg',
  'camera-grid': 'camera-grid.svg',
};

/**
 * The drawing's own box, as the CSS ratio the element keeps. A caller gives
 * one length, a height or a width, and the other follows from this.
 */
export const ART_RATIO: Record<ArtName, string> = {
  'empty-fleet': '240 / 160',
  fractal: '160 / 120',
  shelf: '240 / 24',
  'camera-grid': '1 / 1',
};

/** What the drawing is of, read out by a screen reader. */
export const ART_ALT_KEY: Record<ArtName, string> = {
  'empty-fleet': 'art.emptyFleet',
  fractal: 'art.fractal',
  shelf: 'art.shelf',
  'camera-grid': 'art.cameraGrid',
};
