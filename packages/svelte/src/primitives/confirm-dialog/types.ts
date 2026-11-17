import type { Snippet } from 'svelte';

/** A piece of a question: a plain string, or markup the caller draws. */
export type ConfirmText = string | Snippet;

export interface ConfirmDialogProps {
  /**
   * For a dialog the screen holds open. Leave it and `onOpenChange` out when
   * passing a `trigger`.
   */
  open?: boolean | undefined;
  onOpenChange?: ((open: boolean) => void) | undefined;
  /**
   * The control that opens it, for a dialog whose button is on the screen. It
   * receives the trigger's own props, so the caller's button is the trigger
   * rather than a button inside one.
   */
  trigger?: Snippet<[{ props: Record<string, unknown> }]> | undefined;
  title: ConfirmText;
  /** What will happen, and that it cannot be undone. */
  description: ConfirmText;
  /** Anything between the description and the footer: the arguments a command takes. */
  children?: Snippet | undefined;
  /** The verb, never "OK": Delete, Revoke, Flash, Stop the print. */
  confirmLabel: ConfirmText;
  onConfirm: () => void;
  /**
   * Holds the act back while what it would act on is still being read, so a
   * press cannot confirm something the person has not seen yet.
   */
  confirmDisabled?: boolean | undefined;
  /**
   * `destructive`, the default, for an act that loses something: the red
   * button. `confirm` for one that loses nothing, such as an install or an
   * update: the primary button.
   */
  variant?: ConfirmVariant | undefined;
}

/** How heavy the act a dialog confirms is. */
export type ConfirmVariant = 'destructive' | 'confirm';
