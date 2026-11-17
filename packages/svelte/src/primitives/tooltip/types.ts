import type { Tooltip as TooltipPrimitive } from 'bits-ui';

export type TooltipProviderProps = TooltipPrimitive.ProviderProps;

export type TooltipProps = TooltipPrimitive.RootProps;

export type TooltipTriggerProps = TooltipPrimitive.TriggerProps;

// The element is drawn by the component's own `child` snippet, so a caller
// cannot supply one of its own.
export type TooltipContentProps = Omit<TooltipPrimitive.ContentProps, 'child'>;
