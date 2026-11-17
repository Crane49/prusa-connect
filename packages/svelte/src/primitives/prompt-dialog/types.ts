export interface PromptDialogProps {
  open?: boolean | undefined;
  /**
   * Every path that shuts the panel reports through this: Cancel, Escape, the
   * close control and a click on the wash. Reset `value` here, so a name
   * typed and then abandoned never comes back filled in.
   */
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string | undefined;
  /** The name of the field, drawn above it. */
  label: string;
  placeholder?: string | undefined;
  /** The input type. A secret takes `password`; the default is plain text. */
  type?: 'text' | 'password' | undefined;
  autocomplete?: string | undefined;
  value: string;
  onValueChange: (value: string) => void;
  /**
   * The caller's own verdict on `value`, a name already taken say. It is drawn
   * under the field and it blocks the confirm button.
   */
  error?: string | undefined;
  /**
   * Whether an empty field may be confirmed. A name that falls back to a
   * default when it is cleared takes this.
   */
  allowEmpty?: boolean | undefined;
  /** The confirm has been sent and its answer has not arrived. */
  pending?: boolean | undefined;
  /** The verb, never "OK": Create, Rename. */
  confirmLabel: string;
  onConfirm: () => void;
}
