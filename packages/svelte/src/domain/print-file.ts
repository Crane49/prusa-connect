// A printer's listing carries no printable flag, so the name is what says it.
const PRINT_FILE = /\.(?:bgcode|gcode|gco|g)$/i;

/** Whether a file name is a print file: one a printer starts a job from. */
export const isPrintFile = (name: string): boolean => PRINT_FILE.test(name);

/** The listing order: folders first, then the newest, then by name. */
export const newestFirst =
  <T extends { folder: boolean; modified?: string }>(
    nameOf: (entry: T) => string,
  ) =>
  (a: T, b: T): number => {
    // An entry the printer gave no time for sorts after every dated one.
    const time = (entry: T): number => {
      const ms = Date.parse(entry.modified ?? '');

      return Number.isNaN(ms) ? Number.MIN_SAFE_INTEGER : ms;
    };
    const ta = time(a);
    const tb = time(b);

    return (
      Number(b.folder) - Number(a.folder) ||
      tb - ta ||
      nameOf(a).localeCompare(nameOf(b))
    );
  };
