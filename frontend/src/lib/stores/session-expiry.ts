// What the app does when the session ends under a page that was open: ask the
// session store once, then carry the address across the sign-in.
import type { SignInFailure } from '@prusa/connect-ui/shell';

// Any origin serves to test a path against. Only the comparison reads it.
const BASE = 'http://return.invalid';

/**
 * The address `next` names when it stays on this origin, and `undefined`
 * otherwise. Sign-in sends the person there, so a value that leaves the origin
 * would be an open redirect: `//host`, `/\host` and `https://host` all
 * resolve to another origin and are refused.
 */
export const safeNext = (next: string | null): string | undefined => {
  if (!next?.startsWith('/')) return undefined;
  // Browsers read a backslash as a slash, and strip tabs and newlines inside
  // a URL, so `/\host` and `/\t/host` are `//host` to them.
  for (let at = 0; at < next.length; at += 1) {
    if (next[at] === '\\' || next.charCodeAt(at) < 0x20) return undefined;
  }
  if (next.startsWith('//')) return undefined;

  const url = new URL(next, BASE);

  return url.origin === BASE ? url.pathname + url.search + url.hash : undefined;
};

const NEXT = 'next';

/**
 * Where the page goes when the session ended under it: the sign-in carrying
 * the address that was open. The sign-in is drawn at `home`, so a page that
 * was already there carries nothing.
 */
export const addressAfterExpiry = (
  home: string,
  path: string,
  search: string,
): string => {
  if (path === home) return home;

  return `${home}?${new URLSearchParams({ [NEXT]: path + search })}`;
};

/**
 * The query a sign-in through an identity provider keeps for the way back.
 * The provider returns the browser to `home`, so an address other than `home`
 * travels as `next`, as it does after an expiry: a link such as
 * `/settings/extensions?install=…` opens again after the sign-in.
 */
export const searchBeforeSignIn = (
  home: string,
  path: string,
  search: string,
): string => {
  if (path === home) return search;

  return `?${new URLSearchParams({ [NEXT]: path + search })}`;
};

/**
 * Where a person who just signed in goes: the address `next` carries, `home`
 * when it carries one that is not safe, and `undefined` when it carries none.
 */
export const addressAfterSignIn = (
  home: string,
  search: string,
): string | undefined => {
  const next = new URLSearchParams(search).get(NEXT);
  if (next === null) return undefined;

  return safeNext(next) ?? home;
};

/** What the expiry handler asks of the session store. */
export interface Rereadable {
  readonly status: string;
  read(): Promise<void>;
}

/**
 * The one answer to the server saying the credential stopped being accepted,
 * whether it said so with a 401 or with `auth.expired` on the stream. The
 * session store asks who this browser is, and when the answer is nobody it
 * shows the sign-in. Refusals that arrive while that question is open join it.
 */
export const rereadOnRefusal = (session: Rereadable): (() => void) => {
  let asking = false;

  return () => {
    if (session.status !== 'signed-in' || asking) return;
    asking = true;
    void session.read().finally(() => {
      asking = false;
    });
  };
};

const SIGN_IN_ERROR = 'sign_in_error';

const FAILURES: readonly SignInFailure[] = [
  'provider',
  'id_token',
  'subject',
  'expired',
  'failed',
];

/**
 * Why the last sign-in at the identity provider did not finish, when the
 * server sent the browser back with `sign_in_error`. A reason this page does
 * not know reads as `failed`, so a newer server still gets a sentence.
 */
export const signInFailureOf = (search: string): SignInFailure | undefined => {
  const named = new URLSearchParams(search).get(SIGN_IN_ERROR);
  if (named === null) return undefined;

  return FAILURES.find((failure) => failure === named) ?? 'failed';
};

/**
 * The address without `sign_in_error`, so a reload does not show the
 * sentence again.
 */
export const withoutSignInFailure = (path: string, search: string): string => {
  const query = new URLSearchParams(search);
  query.delete(SIGN_IN_ERROR);
  const rest = query.toString();

  return rest === '' ? path : `${path}?${rest}`;
};
