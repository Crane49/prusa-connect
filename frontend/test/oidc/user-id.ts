// The user id the core gives a person who signs in at an issuer, written here
// a second time and apart from the core's own code, so the lane checks the
// rule and not the core against itself. ADR-20261023-wgv states it:
//
//   - a subject that is already a user id, and does not start with `oidc-`,
//     is the id as it is
//   - any other subject is `oidc-` and the first 32 characters of the
//     lowercase base32 SHA-256 of the issuer, a NUL and the subject
import { createHash } from 'node:crypto';

const USER_ID = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
const PREFIX = 'oidc-';
const BASE32 = 'abcdefghijklmnopqrstuvwxyz234567';

const base32 = (bytes: Uint8Array): string => {
  let out = '';
  let buffer = 0;
  let bits = 0;
  for (const byte of bytes) {
    buffer = (buffer << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      out += BASE32.charAt((buffer >>> (bits - 5)) & 31);
      bits -= 5;
    }
  }
  if (bits > 0) out += BASE32.charAt((buffer << (5 - bits)) & 31);

  return out;
};

/** The user id a subject at an issuer maps to. */
export const userIdOf = (issuer: string, subject: string): string => {
  if (USER_ID.test(subject) && !subject.startsWith(PREFIX)) return subject;

  const hash = createHash('sha256')
    .update(`${issuer.replace(/\/$/, '')}\u0000${subject}`)
    .digest();

  return PREFIX + base32(hash).slice(0, 32);
};
