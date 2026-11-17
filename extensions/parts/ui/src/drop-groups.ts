// How dropped files are grouped into parts. Plain functions, so the store and
// its tests share them without any reactive state.
import { clipBytes, MAX_NAME } from './presentation';
import { segmentOf } from './version-draft.svelte';

/** A file name without its last extension, and the whole name when it has none. */
export const stemOf = (name: string): string => {
  const dot = name.lastIndexOf('.');
  const stem = dot > 0 ? name.slice(0, dot) : name;

  // The module counts the limit in UTF-8 bytes, not in characters.
  return clipBytes(stem.trim(), MAX_NAME) || clipBytes(name, MAX_NAME);
};

/** The extension of one file name, lower case and with its dot. */
export const extensionOf = (name: string): string => {
  const dot = name.lastIndexOf('.');

  return dot > 0 ? name.slice(dot).toLowerCase() : '';
};

/**
 * The dropped files grouped into parts, in the order they were dropped. Two
 * files that land on one path in the part, the same name dropped twice, are
 * one file: the later replaces the earlier, as the storage would, and the
 * module refuses a version that names one file twice.
 */
export const groupByStem = (
  files: readonly File[],
): readonly { name: string; files: readonly File[] }[] => {
  const groups = new Map<string, { name: string; files: File[] }>();

  for (const file of files) {
    const name = stemOf(file.name);
    const key = name.toLowerCase();
    const group = groups.get(key);
    if (group === undefined) {
      groups.set(key, { name, files: [file] });
      continue;
    }

    // Compared without case, because a storage on a disk that ignores case
    // writes Hook.stl and hook.stl to one file.
    const segment = segmentOf(file.name).toLowerCase();
    const same = group.files.findIndex(
      (held) => segmentOf(held.name).toLowerCase() === segment,
    );
    if (same === -1) group.files.push(file);
    else group.files[same] = file;
  }

  return [...groups.values()];
};
