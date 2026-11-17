import type { Popover as PopoverPrimitive } from 'bits-ui';
import type { HTMLAttributes } from 'svelte/elements';

export type PopoverProps = PopoverPrimitive.RootProps;

export type PopoverTriggerProps = PopoverPrimitive.TriggerProps;

export type PopoverContentProps = PopoverPrimitive.ContentProps;

export type PopoverHeaderProps = HTMLAttributes<HTMLDivElement>;

export type PopoverTitleProps = HTMLAttributes<HTMLDivElement>;

export type PopoverDescriptionProps = HTMLAttributes<HTMLParagraphElement>;
