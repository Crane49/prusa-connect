import type { Spool } from '../spools';

/**
 * What a module that draws the picker passes it. It is part of what this
 * module publishes: a change a caller would break on is a new major release.
 */
export interface SpoolPickerOptions {
  /** Called with the spool the person picked. */
  onselect: (spool: Spool) => void;
  /** The id of the spool drawn as picked, if any. */
  selected?: string | undefined;
}

/**
 * The same props as `host.components.load` takes them: a component's props
 * there are a record of named values.
 */
export type SpoolPickerProps = SpoolPickerOptions & Record<string, unknown>;
