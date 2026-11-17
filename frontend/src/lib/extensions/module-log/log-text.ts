import type { ResourceLog } from '../../stores/module-operations.svelte';

/** The lines as the module wrote them: a line cut for its length is joined to the next one. */
export const logText = (log: ResourceLog): string =>
  log.lines
    .map((line) => (line.split === true ? line.text : `${line.text}\n`))
    .join('')
    .trimEnd();
