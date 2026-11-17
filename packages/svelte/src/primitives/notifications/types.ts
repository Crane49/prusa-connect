import type { ClassValue } from 'svelte/elements';

import type { NotificationKind } from '../../domain';
import type { PopoverContentProps } from '../popover/types';
import type { PrinterModel } from '../printer-icon/types';

export interface NotificationAction {
  id: string;
  label: string;
  variant?: 'default' | 'secondary' | 'destructive';
  /** Where the action goes. With it the action is a link; without it, a button. */
  href?: string | undefined;
}

export interface NotificationItem {
  id: string;
  /** Which backend fact this is. The tone, the glyph and the word follow from it. */
  kind: NotificationKind;
  title: string;
  body?: string;
  /**
   * Ready to draw, `09:41` or `Yesterday`, never a `Date`, so a story and a
   * screenshot stay reproducible.
   */
  at: string;
  read: boolean;
  /** The machine it happened to, drawn at the row's leading edge. */
  printerModel?: PrinterModel;
  /** The sliced plate, for the rows that are about a file. */
  thumbnailUrl?: string;
  actions?: NotificationAction[];
}

export interface NotificationsProps {
  items: NotificationItem[];
  /**
   * How many printers are asking for a person right now, which is a state and
   * not an event. It marks the bell with its own count and pins the panel's
   * first row. Zero or omitted draws neither.
   */
  needsAttention?: number | undefined;
  onToggleRead?: ((id: string) => void) | undefined;
  onMarkAllRead?: (() => void) | undefined;
  onAction?: ((notificationId: string, actionId: string) => void) | undefined;
  triggerSize?: 'icon-md' | 'icon-lg' | undefined;
  presentation?: 'popover' | 'sheet' | undefined;
  /**
   * Which edge the panel comes from, in both presentations. For a popover it
   * is which side of the trigger it sits on; for a sheet it is which edge of
   * the screen it is anchored to, which is the edge its own bar is docked to.
   */
  side?: PopoverContentProps['side'];
  align?: PopoverContentProps['align'];
  /** Where the sheet docks. That is the caller's, see the component's header. */
  sheetClass?: ClassValue | undefined;
  /** Where the sheet's scrim stops, so a bar stays visible over it. */
  sheetOverlayClass?: ClassValue | undefined;
  class?: ClassValue | undefined;
}
