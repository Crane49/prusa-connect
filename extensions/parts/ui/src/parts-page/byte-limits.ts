// Text fields held to the module's limits, which count UTF-8 bytes.
//
// An input's maxlength counts characters, and a name typed with accents is
// longer in bytes than in characters, so maxlength lets through what the
// module then refuses. A field says its limit in `data-byte-limit`, and the
// page keeps every such field within it while the person types or pastes: an
// edit that would cross the limit keeps as much of what was typed as fits,
// cut between two characters and never inside one.
//
// One listener on the document, in the capture phase, so it sees every field
// of the page and of its dialogs, which are drawn outside the page, before the
// field's own handlers read the value.
import type { Attachment } from 'svelte/attachments';

import { byteLength, clipBytes } from '../presentation';

/** The edit as it should land: the value, and where the caret goes. */
export interface Fitted {
  readonly value: string;
  readonly caret: number;
}

/**
 * Fits one edit into `limit` bytes. `before` is the value and `start`/`end`
 * the selection the edit replaced; `after` is the value the edit produced.
 * Answers undefined when the edit already fits. The text around the edit is
 * kept whole, and the inserted text is cut to the room that is left.
 */
export const fitEdit = (
  before: string,
  start: number,
  end: number,
  after: string,
  limit: number,
): Fitted | undefined => {
  if (byteLength(after) <= limit) return undefined;

  const head = before.slice(0, start);
  const tail = before.slice(end);
  const shaped = after.startsWith(head) && after.endsWith(tail);

  // An edit this cannot read as one insertion, such as a value set by a
  // script, keeps its start.
  if (!shaped || after.length < head.length + tail.length) {
    const value = clipBytes(after, limit);

    return { value, caret: value.length };
  }

  const inserted = after.slice(head.length, after.length - tail.length);
  const room = Math.max(0, limit - byteLength(head) - byteLength(tail));
  const kept = clipBytes(inserted, room);

  return { value: head + kept + tail, caret: head.length + kept.length };
};

type TextField = HTMLInputElement | HTMLTextAreaElement;

const limitOf = (target: EventTarget | null): number | undefined => {
  if (
    !(target instanceof HTMLInputElement) &&
    !(target instanceof HTMLTextAreaElement)
  ) {
    return undefined;
  }

  const limit = Number(target.dataset.byteLimit);

  return Number.isInteger(limit) && limit > 0 ? limit : undefined;
};

/** Where a field stood just before an edit. */
interface Before {
  readonly value: string;
  readonly start: number;
  readonly end: number;
}

/**
 * Holds every field carrying `data-byte-limit` to its limit, for as long as
 * the element it is attached to is on the page.
 */
export const byteLimits: Attachment<HTMLElement> = (node) => {
  const owner = node.ownerDocument;
  const pending = new WeakMap<TextField, Before>();

  const remember = (event: Event): void => {
    if (limitOf(event.target) === undefined) return;
    const field = event.target as TextField;
    pending.set(field, {
      value: field.value,
      start: field.selectionStart ?? field.value.length,
      end: field.selectionEnd ?? field.value.length,
    });
  };

  const hold = (event: Event): void => {
    const limit = limitOf(event.target);
    if (limit === undefined) return;
    // A composition (an input method building one character from several
    // keys) is fitted once it is done, never in the middle of it.
    if (event instanceof InputEvent && event.isComposing) return;

    const field = event.target as TextField;
    const before = pending.get(field);
    pending.delete(field);

    const fitted =
      before === undefined
        ? fitEdit('', 0, 0, field.value, limit)
        : fitEdit(before.value, before.start, before.end, field.value, limit);
    if (fitted === undefined) return;

    field.value = fitted.value;
    field.setSelectionRange(fitted.caret, fitted.caret);
  };

  owner.addEventListener('beforeinput', remember, true);
  owner.addEventListener('input', hold, true);

  return () => {
    owner.removeEventListener('beforeinput', remember, true);
    owner.removeEventListener('input', hold, true);
  };
};
