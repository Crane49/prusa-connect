import type { AlertDialog as AlertDialogPrimitive } from 'bits-ui';
import type { HTMLAttributes } from 'svelte/elements';

import type { ButtonProps, ButtonTextSize } from '../button/types';

/** How wide the panel gets on a wide window. */
export type AlertDialogSize = 'default' | 'sm';

export type AlertDialogProps = AlertDialogPrimitive.RootProps;

export type AlertDialogTriggerProps = AlertDialogPrimitive.TriggerProps;

export type AlertDialogPortalProps = AlertDialogPrimitive.PortalProps;

export type AlertDialogOverlayProps = AlertDialogPrimitive.OverlayProps;

export type AlertDialogContentProps = AlertDialogPrimitive.ContentProps & {
  size?: AlertDialogSize | undefined;
};

export type AlertDialogHeaderProps = HTMLAttributes<HTMLDivElement>;

export type AlertDialogFooterProps = HTMLAttributes<HTMLDivElement>;

export type AlertDialogMediaProps = HTMLAttributes<HTMLDivElement>;

export type AlertDialogTitleProps = AlertDialogPrimitive.TitleProps;

export type AlertDialogDescriptionProps = AlertDialogPrimitive.DescriptionProps;

export type AlertDialogActionProps = AlertDialogPrimitive.ActionProps &
  Pick<ButtonProps, 'variant'> & { size?: ButtonTextSize | undefined };

export type AlertDialogCancelProps = AlertDialogPrimitive.CancelProps &
  Pick<ButtonProps, 'variant'> & { size?: ButtonTextSize | undefined };
