import { getContext, setContext } from 'svelte';

/**
 * What the server's device-model vocabulary says about a model this library
 * has no artwork for: the name it is sold under, and where the server serves a
 * drawing of it when a driver declared one. Most often a driver declared the
 * model (ADR-20261016-qrp).
 */
export interface DeclaredModel {
  name: string;
  src?: string;
}

/** Finds the vocabulary's row for a model by the spelling the device reports. */
export type DeclaredModels = (reported: string) => DeclaredModel | undefined;

/** Exported for a test that mounts the icon with its own lookup. */
export const DECLARED_MODELS = Symbol('printer-icon-declared-models');

/**
 * How the icons beneath the caller look up a model they have no artwork for.
 * The application passes a reader over the device-model vocabulary, which this
 * tier cannot import.
 */
export const setDeclaredModels = (lookup: DeclaredModels): void => {
  setContext(DECLARED_MODELS, lookup);
};

export const declaredModels = (): DeclaredModels | undefined =>
  getContext<DeclaredModels | undefined>(DECLARED_MODELS);
