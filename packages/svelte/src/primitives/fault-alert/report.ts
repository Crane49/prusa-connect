import { getContext, setContext } from 'svelte';

const KEY = Symbol('fault-alert-report');

/**
 * Where a FaultAlert's "Report a problem" link points for the components
 * beneath the caller. `undefined` takes the link away, for the page it would
 * point at.
 */
export const setReportHref = (href: string | undefined): void => {
  setContext(KEY, href);
};

export const reportHref = (): string | undefined =>
  getContext<string | undefined>(KEY);
