// Shared by the module's build and its dev server, so both serve the same CSS.

/**
 * A registration and a layer-order statement are only valid at the top level of
 * a stylesheet, and a browser drops one it meets inside `@scope`. Tailwind emits
 * `@property` for the custom properties its transform, shadow and gradient
 * utilities animate.
 */
const TOP_LEVEL_ONLY = /@property\s[^{]+\{[^}]*\}|@layer\s[^{;]+;/g;

/**
 * The scope prelude of any module's markup. A bundle narrows it to its own
 * module once it knows its namespace (ADR-20261029-zzg), and keeps it where it
 * cannot tell, such as in dev.
 */
export const ANY_MODULE = '([data-module])';

/**
 * Wraps a module's CSS in `@scope`, so its rules reach only the markup inside
 * a module's slot (ADR-20260914-244 rule 14). The host's utilities sit in the
 * same `utilities` layer and earlier in the page, so an unscoped `.flex-col`
 * from a module beats the host's `@[40rem]:flex-row` on every host element.
 *
 * The wrapping happens here and not in the module's stylesheet so that no way of
 * writing that stylesheet can reach the host. The scope is `ANY_MODULE` unless
 * a prelude is named.
 */
export const scopedToTheModule = (css: string, scope = ANY_MODULE): string => {
  const hoisted: string[] = [];
  const rules = css.replace(TOP_LEVEL_ONLY, (rule) => {
    hoisted.push(rule);
    return '';
  });

  return `${hoisted.join('')}@scope ${scope}{${rules}}`;
};
