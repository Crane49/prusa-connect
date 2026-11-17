// The setup wizard's lines join the core's catalogue only when the wizard's
// own chunk loads. A server draws the wizard once, on its first run, so every
// page after it would otherwise download and parse them in every language for
// nothing.
import { CORE_NAMESPACE, locale } from '../../i18n';
import { WIZARD_CATALOGUES } from './catalogues';

let added = false;

/** Adds the wizard's lines to the core's catalogue, once per page. */
export const addWizardTexts = (): void => {
  if (added) return;
  added = true;

  for (const [tag, messages] of Object.entries(WIZARD_CATALOGUES)) {
    locale.extend(CORE_NAMESPACE, tag, messages);
  }
};
