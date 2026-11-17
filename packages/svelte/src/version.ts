/**
 * The version of this library, which the module loader checks a bundle's
 * `library` range against before it imports the bundle (ADR-20260911-4p6 rule 24).
 *
 * It is a constant rather than a read of `package.json`, because the file
 * would then be inlined into every built chunk together with the whole
 * dependency list. `version.test.ts` holds the constant equal to the manifest,
 * so the two cannot drift.
 */
export const LIBRARY_VERSION = '0.1.0';
