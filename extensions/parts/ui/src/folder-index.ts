// The folders of one overview, indexed by id and by the folder they are in.
//
// Plain TypeScript and not a store: the catalogue builds one index per read
// of the overview inside a `$derived` and replaces it whole with the next
// read, so the maps are never changed after they are built and need no
// reactivity of their own.
import type { Folder } from './parts';

/** No folder, answered for a folder that holds none, so every caller gets the same empty list. */
export const NO_FOLDERS: readonly Folder[] = [];

/** The folders of one overview, looked up by id and by the folder they are in. */
export interface FolderIndex {
  readonly byId: ReadonlyMap<number, Folder>;
  readonly children: ReadonlyMap<number, readonly Folder[]>;
}

/**
 * Indexes the folders of one overview in one pass. The overview orders them
 * by parent and then by name, and each list of children keeps that order.
 */
export const indexOf = (folders: readonly Folder[]): FolderIndex => {
  const byId = new Map<number, Folder>();
  const children = new Map<number, Folder[]>();

  for (const folder of folders) {
    byId.set(folder.id, folder);
    const siblings = children.get(folder.parent);
    if (siblings === undefined) children.set(folder.parent, [folder]);
    else siblings.push(folder);
  }

  return { byId, children };
};
