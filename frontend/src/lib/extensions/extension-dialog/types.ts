import type { ExtensionRow } from '../rows';

/** What a person asked to do with one row. */
export type AskingAct = 'install' | 'update' | 'remove' | 'enable';

export interface Asking {
  readonly row: ExtensionRow;
  readonly act: AskingAct;
  /** The release a link named, when it is not the one the row offers. */
  readonly version?: string | undefined;
}
