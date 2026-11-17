import type { Folder } from '../parts';

/** What the folder dialog is doing, and on which folder. */
export type FolderAct =
  { kind: 'create'; parent: number } | { kind: 'change'; folder: Folder };
