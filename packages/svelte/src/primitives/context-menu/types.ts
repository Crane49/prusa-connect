import type { ContextMenu as ContextMenuPrimitive } from 'bits-ui';
import type { Snippet } from 'svelte';
import type { HTMLAttributes } from 'svelte/elements';

/** How a row is painted. `destructive` marks a command that removes something. */
export type ContextMenuItemVariant = 'default' | 'destructive';

/** A row indented to the width of the tick column, so it lines up with one. */
interface Inset {
  inset?: boolean;
}

export type ContextMenuProps = ContextMenuPrimitive.RootProps;

export type ContextMenuTriggerProps = ContextMenuPrimitive.TriggerProps;

export type ContextMenuPortalProps = ContextMenuPrimitive.PortalProps;

export type ContextMenuContentProps = ContextMenuPrimitive.ContentProps;

export type ContextMenuGroupProps = ContextMenuPrimitive.GroupProps;

export type ContextMenuRadioGroupProps = ContextMenuPrimitive.RadioGroupProps;

export type ContextMenuSubProps = ContextMenuPrimitive.SubProps;

export type ContextMenuSubContentProps = ContextMenuPrimitive.SubContentProps;

export type ContextMenuItemProps = ContextMenuPrimitive.ItemProps &
  Inset & {
    variant?: ContextMenuItemVariant;
  };

export type ContextMenuSubTriggerProps = ContextMenuPrimitive.SubTriggerProps &
  Inset;

export type ContextMenuSeparatorProps = ContextMenuPrimitive.SeparatorProps;

// The tick and the dot are drawn by the component, so a caller supplies plain
// children and bits-ui's state-carrying snippet stays internal.
export type ContextMenuCheckboxItemProps = Omit<
  ContextMenuPrimitive.CheckboxItemProps,
  'child' | 'children'
> & {
  children?: Snippet;
};

export type ContextMenuRadioItemProps = Omit<
  ContextMenuPrimitive.RadioItemProps,
  'child' | 'children'
> & {
  children?: Snippet;
};

export type ContextMenuLabelProps = HTMLAttributes<HTMLDivElement> & Inset;

export type ContextMenuShortcutProps = HTMLAttributes<HTMLSpanElement>;
