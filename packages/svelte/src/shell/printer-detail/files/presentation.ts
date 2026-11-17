// What a file on a printer's drive is.
import type { Component } from 'svelte';

import FileIcon from '@lucide/svelte/icons/file';
import FileCode2Icon from '@lucide/svelte/icons/file-code-2';
import FolderIcon from '@lucide/svelte/icons/folder';

import { isPrintFile } from '../../../domain';

import type { PrinterFileEntry } from './types';

export const SPLIT_ROW =
  'flex flex-col @5xl/printer-files:flex-row @5xl/printer-files:items-stretch';

export const SPLIT_LEADING =
  'border-t border-border pt-6 @5xl/printer-files:order-first @5xl/printer-files:w-1/3 @5xl/printer-files:min-w-64 @5xl/printer-files:border-t-0 @5xl/printer-files:pt-0 @5xl/printer-files:pe-6';

export const SPLIT_TRAILING =
  'border-border pb-6 @5xl/printer-files:border-s @5xl/printer-files:pb-0 @5xl/printer-files:ps-6';

/** A firmware image the printer can write to itself. */
const FIRMWARE = /\.bbf$/i;
/** A model the printer stores and cannot start. */
const MODEL = /\.(?:3mf|stl|obj)$/i;

export const nameOf = (entry: PrinterFileEntry): string =>
  entry.display_name ?? entry.name;

export const isPrintable = (entry: PrinterFileEntry): boolean =>
  !entry.folder && isPrintFile(nameOf(entry));

/** Whether a file of this name is a firmware image. */
export const isFirmwareName = (name: string): boolean => FIRMWARE.test(name);

export const isFirmware = (entry: PrinterFileEntry): boolean =>
  !entry.folder && isFirmwareName(nameOf(entry));

export const isModel = (entry: PrinterFileEntry): boolean =>
  !entry.folder && MODEL.test(nameOf(entry));

export const glyphOf = (entry: PrinterFileEntry): Component => {
  if (entry.folder) return FolderIcon;

  return isPrintable(entry) ? FileCode2Icon : FileIcon;
};
