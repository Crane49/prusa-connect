import type { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';
import type { Snippet } from 'svelte';
import type { HTMLAttributes } from 'svelte/elements';

/** How a row is painted. `destructive` marks a command that removes something. */
export type DropdownMenuItemVariant = 'default' | 'destructive';

/** A row indented to the width of the tick column, so it lines up with one. */
interface Inset {
  inset?: boolean;
}

export type DropdownMenuProps = DropdownMenuPrimitive.RootProps;

export type DropdownMenuTriggerProps = DropdownMenuPrimitive.TriggerProps;

export type DropdownMenuPortalProps = DropdownMenuPrimitive.PortalProps;

export type DropdownMenuContentProps = DropdownMenuPrimitive.ContentProps;

export type DropdownMenuGroupProps = DropdownMenuPrimitive.GroupProps;

export type DropdownMenuRadioGroupProps = DropdownMenuPrimitive.RadioGroupProps;

export type DropdownMenuSubProps = DropdownMenuPrimitive.SubProps;

export type DropdownMenuSubContentProps = DropdownMenuPrimitive.SubContentProps;

export type DropdownMenuItemProps = DropdownMenuPrimitive.ItemProps &
  Inset & {
    variant?: DropdownMenuItemVariant;
  };

export type DropdownMenuSubTriggerProps =
  DropdownMenuPrimitive.SubTriggerProps & Inset;

export type DropdownMenuSeparatorProps = DropdownMenuPrimitive.SeparatorProps;

// The tick and the dot are drawn by the component, so a caller supplies plain
// children and bits-ui's state-carrying snippet stays internal.
export type DropdownMenuCheckboxItemProps = Omit<
  DropdownMenuPrimitive.CheckboxItemProps,
  'child' | 'children'
> & {
  children?: Snippet;
};

export type DropdownMenuRadioItemProps = Omit<
  DropdownMenuPrimitive.RadioItemProps,
  'child' | 'children'
> & {
  children?: Snippet;
};

export type DropdownMenuLabelProps = HTMLAttributes<HTMLDivElement> & Inset;

export type DropdownMenuShortcutProps = HTMLAttributes<HTMLSpanElement>;
