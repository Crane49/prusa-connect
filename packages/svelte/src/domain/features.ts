export type Feature = 'tags' | 'diagnostics';

/** Parts of the interface that are built but not shown. ADR-20260930-rq4 says why. */
export const FEATURES: Readonly<Record<Feature, boolean>> = {
  tags: false,
  diagnostics: true,
};
