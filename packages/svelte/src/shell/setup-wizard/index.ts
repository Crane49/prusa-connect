// The first-run wizard (ADR-20261015-ejz). It is its own subpath, `./setup-wizard`,
// so the application loads it with a dynamic import and a server that is set
// up never sends it, nor its texts in every language, to a browser again.
export { default as SetupWizard } from './setup-wizard.svelte';
export type { SetupWizardProps } from './types';
