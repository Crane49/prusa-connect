// This reads the tags on the fleet's own records as a list of tags.
import { tagKey } from '../../domain';
import type { Device } from '../../domain/command';
import { printerHref } from '../printers/presentation';
import type { TagPrinter, TagRow } from './types';

interface Gathered {
  id: string;
  tag: string;
  printers: TagPrinter[];
}

/**
 * Every tag the fleet carries, one row per tag. Two tags with one key are one
 * tag, as the server compares them, and the first spelling is shown.
 */
export const tagRowsOf = (devices: readonly Device[]): TagRow[] => {
  const gathered = new Map<string, Gathered>();

  for (const device of devices) {
    const printer: TagPrinter = {
      id: device.id,
      name: device.name,
      detailHref: printerHref(device.id),
    };

    for (const tag of device.tags ?? []) {
      const id = tagKey(tag);
      const row = gathered.get(id) ?? { id, tag, printers: [] };
      if (!row.printers.some((held) => held.id === device.id)) {
        row.printers.push(printer);
      }
      gathered.set(id, row);
    }
  }

  return [...gathered.values()];
};

/** This orders the rows by the tag as it is shown. */
export const byTag = (a: TagRow, b: TagRow): number =>
  a.tag.localeCompare(b.tag);
