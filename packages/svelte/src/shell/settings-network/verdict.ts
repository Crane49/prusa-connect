/**
 * How the announcer stands, as the network room and the Overview card both
 * say it. An announcement on a bridge network never leaves the container, so
 * it is its own verdict rather than a success.
 */
export const networkVerdict = (
  announcement: string | undefined,
  bridged: boolean | undefined,
): string | undefined =>
  announcement === 'announcing' && bridged === true ? 'bridged' : announcement;
