/** The two lists on one instance of the resources listing that hide entries. */
export interface HidingInstance {
  /** The operator's list, kept in the declaration (ADR-20261021-3dp). */
  readonly hidden_contributions?: readonly string[] | undefined;
  /** The running module's own list, kept while it runs (ADR-20261021-6h2). */
  readonly ui?: { readonly hidden?: readonly string[] | undefined } | undefined;
}

/**
 * Every entry of one module that is hidden now, each as
 * `<slot key>/<identity>`: the operator's entries and the module's own.
 *
 * The two lists are kept apart on the server, so the module can never show
 * an entry the operator hid. Here they are one list, because both hide an
 * entry the same way.
 *
 * It is a leaf of its own, beside `module-href.ts`, because the store that
 * reads the listing and the loader that declares a module to the registry
 * both read it, and neither may import the other.
 */
export const hiddenEntries = (instance: HidingInstance): readonly string[] => {
  const operator = instance.hidden_contributions ?? [];
  const module = instance.ui?.hidden ?? [];
  if (module.length === 0) return operator;

  return [...new Set([...operator, ...module])];
};
