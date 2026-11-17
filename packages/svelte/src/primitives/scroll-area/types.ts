import type { Orientation, ScrollArea as ScrollAreaPrimitive } from 'bits-ui';

/** The root mounts its own viewport, scrollbars and corner, so it takes children alone. */
export type ScrollAreaProps = Omit<ScrollAreaPrimitive.RootProps, 'child'>;

export type ScrollBarProps = Omit<
  ScrollAreaPrimitive.ScrollbarProps,
  'child' | 'children' | 'orientation'
> & {
  orientation?: Orientation;
};
