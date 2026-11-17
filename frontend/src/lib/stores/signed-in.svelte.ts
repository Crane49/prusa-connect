import { untrack } from 'svelte';

/** What the app knows about who is signed in. */
export interface SignedInState {
  readonly status: string;
  readonly principal: { readonly id: string } | undefined;
}

/**
 * Runs `start` while somebody is signed in. Call it while a component is being
 * set up, or inside an effect root.
 *
 * It runs again when the person changes, and the cleanup `start` returned runs
 * first. Another tab can sign in as somebody else without this page
 * reloading, and nothing opened for the first person may carry on for the
 * second. `start` runs untracked, so what it reads does not restart it.
 */
export const whileSignedIn = (
  session: SignedInState,
  start: () => () => void,
): void => {
  // The id is derived, so a session read again that hands over a new object
  // for the same person restarts nothing.
  const person = $derived(session.principal?.id);
  const signedIn = $derived(session.status === 'signed-in');

  $effect(() => {
    if (!signedIn || person === undefined) return;

    return untrack(start);
  });
};

/**
 * Runs `forget` when the person signed in leaves: they sign out, or another
 * person signs in without the page reloading. A first sign-in forgets
 * nothing, because nobody was there before. Call it while a component is
 * being set up, or inside an effect root.
 *
 * It runs before the page is drawn again, and before the work `whileSignedIn`
 * starts for the new person. So no screen is drawn for the new person with
 * what the last one read, and nothing the new person reads is forgotten.
 * `forget` runs untracked, so what it reads does not run it again.
 */
export const forgetEachPerson = (
  session: SignedInState,
  forget: () => void,
): void => {
  const person = $derived(
    session.status === 'signed-in' ? session.principal?.id : undefined,
  );
  let last = untrack(() => person);

  $effect.pre(() => {
    const now = person;
    if (now === last) return;

    const left = last;
    last = now;
    if (left !== undefined) untrack(forget);
  });
};
