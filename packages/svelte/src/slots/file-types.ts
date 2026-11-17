// The file_types filter of a slot handed a file (ADR-20261027-wvj rule 17).
// An entry that names no types is drawn for every file. One that names some
// is drawn for a file whose name ends in one of the extensions, without
// regard to case.
import { parseFileRef } from './file-ref';

/** Whether an entry with these file types is drawn for a file of this name. */
export const matchesFileType = (
  types: readonly string[] | null | undefined,
  name: string,
): boolean => {
  if (types === undefined || types === null || types.length === 0) return true;

  const lower = name.toLowerCase();

  return types.some(
    (type) => lower.length > type.length && lower.endsWith(type),
  );
};

/** Whether an entry with these file types is drawn for the file a reference names. */
export const matchesFileRef = (
  types: readonly string[] | null | undefined,
  fileRef: string,
): boolean => {
  const file = parseFileRef(fileRef);

  return matchesFileType(types, file?.path.split('/').at(-1) ?? '');
};
