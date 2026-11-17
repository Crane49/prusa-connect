import type { Dialog as SheetPrimitive } from 'bits-ui';
import type { ClassValue, HTMLAttributes } from 'svelte/elements';

/** The edge the panel is anchored to, and the edge it comes in from. */
export type SheetSide = 'top' | 'right' | 'bottom' | 'left';

export type SheetProps = SheetPrimitive.RootProps;

export type SheetTriggerProps = SheetPrimitive.TriggerProps;

export type SheetCloseProps = SheetPrimitive.CloseProps;

export type SheetPortalProps = SheetPrimitive.PortalProps;

export type SheetOverlayProps = SheetPrimitive.OverlayProps;

// `exactOptionalPropertyTypes` is on, so an optional prop a caller may forward
// as `undefined` says so. A parent that holds `side` in a variable it has not
// set yet passes `undefined`, and that has to mean the default rather than a
// type error.
export type SheetContentProps = SheetPrimitive.ContentProps & {
  /** The edge the panel is anchored to. The default is `right`. */
  side?: SheetSide | undefined;
  /** Draw the cross in the corner of the panel. */
  showCloseButton?: boolean | undefined;
  /** Classes on the dimming layer, to stop it above a fixed bar. */
  overlayClass?: ClassValue | undefined;
};

export type SheetHeaderProps = HTMLAttributes<HTMLDivElement>;

export type SheetFooterProps = HTMLAttributes<HTMLDivElement>;

export type SheetTitleProps = SheetPrimitive.TitleProps;

export type SheetDescriptionProps = SheetPrimitive.DescriptionProps;
