// What the storage listing asks the server about the files it is drawing.
import type { FolderCrumb } from '../../primitives';

/**
 * A print file: what a slicer writes and what this server can open.
 *
 * The server picks its parser from the first bytes of the file and not from
 * the name (`internal/printfile`), so a text G-code file reads the same under
 * every spelling the slicers use. `.gco` and `.g` are text G-code as much as
 * `.gcode` is.
 *
 * One list, because it answers two questions that must not drift apart: which
 * glyph a row shows, and which file the screen asks the server about. A row
 * drawn as a plate that nothing asks about would be a plate with no picture
 * for no reason a reader can see.
 */
const PRINT_FILE = /\.(?:bgcode|gcode|gco|g|3mf)$/i;

/** Whether this name is a file a slicer wrote. */
export const isPrintFile = (name: string): boolean => PRINT_FILE.test(name);

/**
 * How many files are asked about at the same time.
 *
 * A page holds up to a hundred rows and the server opens each file to answer,
 * so asking about all of them at once puts a hundred requests on a Raspberry
 * Pi that is also feeding a printer. Four keeps the pictures arriving while
 * the reader looks at the top of the page.
 */
export const DESCRIBE_AT_ONCE = 4;

/** What the fan-out needs of the storage store, and no more. */
export interface DescribeSource {
  describe(storage: string, path: string, signal?: AbortSignal): Promise<void>;
}

/**
 * Asks the server about each file in turn, a few at a time.
 *
 * The signal is the screen's. A reader who turns the page abandons the rest of
 * the batch rather than letting it finish into a listing that has moved on.
 *
 * A read that failed is left where it fell. The store already records a file
 * it could not read as one that said nothing, and a failure raised out of a
 * worker would end that worker, shrink the pool for the rest of the page and
 * reject the batch as a whole.
 */
export const describeInTurn = async (
  source: DescribeSource,
  storage: string,
  paths: readonly string[],
  signal: AbortSignal,
): Promise<void> => {
  let next = 0;

  const worker = async (): Promise<void> => {
    while (next < paths.length) {
      if (signal.aborted) return;
      const path = paths[next];
      next += 1;
      if (path === undefined) return;

      try {
        await source.describe(storage, path, signal);
      } catch {
        // The next file is still worth asking about.
      }
    }
  };

  const workers = Math.min(DESCRIBE_AT_ONCE, paths.length);

  await Promise.all(Array.from({ length: workers }, () => worker()));
};

/**
 * The path split into the steps a reader walks back through, the storage's own
 * display name leading. An empty path is the storage root, which is the one
 * crumb every trail starts with.
 */
export const trailOf = (root: string, path: string): FolderCrumb[] => {
  const parts = path.split('/').filter((part) => part !== '');

  return [
    { label: root, path: '' },
    ...parts.map((part, index) => ({
      label: part,
      path: parts.slice(0, index + 1).join('/'),
    })),
  ];
};

/**
 * A name split where a rename leaves it alone: the extension of a file stays
 * and the reader edits the stem. A folder, and a name that only starts with a
 * dot, have no extension.
 */
export const splitName = (
  name: string,
  folder: boolean,
): { stem: string; extension: string } => {
  const dot = name.lastIndexOf('.');
  if (folder || dot <= 0) return { stem: name, extension: '' };

  return { stem: name.slice(0, dot), extension: name.slice(dot) };
};

/** The path of an entry's neighbour in the same folder, under another name. */
export const siblingPath = (path: string, name: string): string =>
  `${path.slice(0, path.lastIndexOf('/') + 1)}${name}`;
