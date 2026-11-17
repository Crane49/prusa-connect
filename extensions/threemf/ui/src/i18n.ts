// The texts of this module, read through the catalogue the host merged under
// its namespace (frontend-i18n.md rule 7).
//
// The project view is drawn on a file's page and inside other modules'
// pages, and both read this module's catalogue, so the components import
// `t` from here and never the library's `t`, which reads the core's alone.
// The setup in `index.ts` hands over `host.t`. Until then, as in a test or a
// story, the same namespace is read directly.
import {
  describeFault as describeCoreFault,
  type FaultText,
  type Translate,
  translateFor,
} from '@prusa/connect-ui/i18n';

export { formatList, formatNumber, formatUnit } from '@prusa/connect-ui/i18n';

/** The namespace the host derives from this module's path. */
export const NAMESPACE = 'com.github.aux.prusa3d.threemf';

let bound: Translate = translateFor(NAMESPACE);

/** Called once by the setup, with the `t` the host bound to this module. */
export const holdTranslate = (translate: Translate): void => {
  bound = translate;
};

/** One text of this module, in the language on screen. */
export const t: Translate = (key, params) => bound(key, params);

/**
 * An error in the words the page shows (frontend-i18n.md rule 5). The
 * library looks a code up in the core's catalogue, and this module's codes,
 * `threemf/...`, are in this module's own. A code with no line here keeps
 * the server's English as the title.
 */
export const describeFault = (thrown: unknown): FaultText => {
  const fault = describeCoreFault(thrown);
  if (fault.code === undefined) return fault;

  const key = `error.${fault.code}`;
  const line = t(key);
  if (line === key) return fault;

  return { code: fault.code, title: line, detail: fault.detail ?? fault.title };
};
