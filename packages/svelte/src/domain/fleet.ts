import type { Device } from './command';

/**
 * What a reader of the fleet may do with it. The host object hands a module
 * this type and never the store itself, so a module reads the fleet and
 * cannot write it (ADR-20260911-4p6 rule 18).
 *
 * It is here and not beside `createFleet` in `../data` because the host
 * object is built a tier below the stores, and a module's own page reads the
 * type without importing a store at all.
 */
export interface ReadonlyFleet {
  /** The rows, replaced wholesale on every change. */
  readonly devices: readonly Device[];
  /** True while a read is in flight. */
  readonly loading: boolean;
  /** True once a read has answered, which tells an empty fleet from an unread one. */
  readonly loaded: boolean;
  /** The last failure, cleared when a read succeeds. */
  readonly error: Error | undefined;
  /** One row by id, or undefined when this caller cannot see that printer. */
  device(id: string): Device | undefined;
}
