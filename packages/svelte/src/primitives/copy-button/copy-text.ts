/**
 * Puts text on the clipboard. `navigator.clipboard` exists only in a secure
 * context, and the product is usually opened over plain `http://<lan-ip>`, so
 * without it the text goes through a hidden textarea and `execCommand`.
 * Rejects when neither route works.
 */
export const copyText = async (text: string): Promise<void> => {
  const api = (navigator as { clipboard?: Clipboard }).clipboard;
  if (api !== undefined) {
    await api.writeText(text);
    return;
  }
  // Selecting the textarea takes focus from the button that was pressed.
  const pressed = document.activeElement;
  const field = document.createElement('textarea');
  field.value = text;
  field.setAttribute('readonly', '');
  field.style.position = 'fixed';
  field.style.opacity = '0';
  document.body.append(field);
  try {
    field.select();
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- the only copy route without a secure context
    if (!document.execCommand('copy')) throw new Error('copy was refused');
  } finally {
    field.remove();
    if (pressed instanceof HTMLElement) pressed.focus();
  }
};
