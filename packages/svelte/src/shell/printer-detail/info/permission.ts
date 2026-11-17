import type { Device } from '../../../domain';

/** The action the server asks for before it renames, tags or retires. */
const ADMINISTER = 'administer';

/**
 * Whether the record says this caller may administer the printer. The server
 * always sends the list, so a record without one allows nothing.
 */
export const mayAdminister = (device: Device): boolean =>
  device.actions?.includes(ADMINISTER) === true;

/**
 * Whether this caller may remove the printer. A printer that runs this server
 * is refused, because it would come back at once under a new id
 * (ADR-20261103-szc).
 */
export const mayRemove = (device: Device): boolean =>
  mayAdminister(device) && device.resident !== true;

/** The action the server asks for before it sends the printer a command. */
const OPERATE = 'operate';

/** Whether the record says this caller may command the printer. */
export const mayOperate = (device: Device): boolean =>
  device.actions?.includes(OPERATE) === true;
