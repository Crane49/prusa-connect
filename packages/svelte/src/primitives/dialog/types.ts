import type { Dialog as DialogPrimitive } from 'bits-ui';
import type { HTMLAttributes } from 'svelte/elements';

export type DialogProps = DialogPrimitive.RootProps;

export type DialogTriggerProps = DialogPrimitive.TriggerProps;

export type DialogPortalProps = DialogPrimitive.PortalProps;

export type DialogCloseProps = DialogPrimitive.CloseProps;

export type DialogOverlayProps = DialogPrimitive.OverlayProps;

/**
 * How wide the panel grows on a screen that has the room: `md` for a form,
 * `lg` for a form with a list or a table, `xl` for two columns side by side.
 */
export type DialogSize = 'md' | 'lg' | 'xl';

export type DialogContentProps = DialogPrimitive.ContentProps & {
  /** Draw the close control in the corner of the panel. */
  showCloseButton?: boolean;
  /**
   * The panel's width. It is a prop rather than a class a caller adds: a
   * module's classes reach no further than its own slot, and the panel is
   * drawn outside it.
   */
  size?: DialogSize;
};

export type DialogHeaderProps = HTMLAttributes<HTMLDivElement>;

export type DialogFooterProps = HTMLAttributes<HTMLDivElement> & {
  /** Add a secondary button that shuts the panel, after the caller's own. */
  showCloseButton?: boolean;
};

export type DialogTitleProps = DialogPrimitive.TitleProps;

export type DialogDescriptionProps = DialogPrimitive.DescriptionProps;
