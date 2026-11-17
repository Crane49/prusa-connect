// An element files can be dropped onto.
//
// It is an attachment, so the listeners leave with the element (svelte-5.md).
// While files are dragged over the element it carries `data-dropping`, which
// the element's own utilities style, so nothing here is a `$state` that would
// redraw the component on every `dragover`.
//
// Only a drag that carries files is taken. A link or a piece of text dragged
// across the page passes over and is left to the browser.
import type { Attachment } from 'svelte/attachments';

/** Whether a drag carries files from outside the page. */
const carriesFiles = (event: DragEvent): boolean =>
  event.dataTransfer?.types.includes('Files') ?? false;

/**
 * Takes files dropped onto the element and hands them to `onFiles`. With
 * `enabled` false the element is no target and a drop is left to the browser.
 */
export const fileDrop =
  (onFiles: (files: File[]) => void, enabled = true): Attachment<HTMLElement> =>
  (node) => {
    if (!enabled) return;

    // dragenter and dragleave fire for every child the pointer crosses, so
    // the element counts them rather than clearing its mark on the first one.
    let depth = 0;

    const clear = (): void => {
      depth = 0;
      delete node.dataset.dropping;
    };

    const enter = (event: DragEvent): void => {
      if (!carriesFiles(event)) return;
      event.preventDefault();
      depth += 1;
      node.dataset.dropping = '';
    };

    const over = (event: DragEvent): void => {
      if (!carriesFiles(event)) return;
      // The drop is only delivered to an element that took the dragover. A
      // target nested in another one takes the files for itself.
      event.preventDefault();
      event.stopPropagation();
      if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
    };

    const leave = (event: DragEvent): void => {
      if (!carriesFiles(event)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) delete node.dataset.dropping;
    };

    const drop = (event: DragEvent): void => {
      if (!carriesFiles(event)) return;
      event.preventDefault();
      event.stopPropagation();
      clear();
      const files = Array.from(event.dataTransfer?.files ?? []);
      if (files.length > 0) onFiles(files);
    };

    node.addEventListener('dragenter', enter);
    node.addEventListener('dragover', over);
    node.addEventListener('dragleave', leave);
    node.addEventListener('drop', drop);
    // A drag that ends anywhere else, or is cancelled with Escape, leaves no
    // mark behind. The window listens in the capture phase, because a target
    // nested in this one stops the drop before it bubbles out.
    window.addEventListener('dragend', clear, true);
    window.addEventListener('drop', clear, true);

    return () => {
      node.removeEventListener('dragenter', enter);
      node.removeEventListener('dragover', over);
      node.removeEventListener('dragleave', leave);
      node.removeEventListener('drop', drop);
      window.removeEventListener('dragend', clear, true);
      window.removeEventListener('drop', clear, true);
      delete node.dataset.dropping;
    };
  };
