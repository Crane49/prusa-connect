// Who is signed in, and what they hold. A module's page hides what the person
// may not do, and this is what it reads to decide (ADR-20260911-4p6 rule 18).
import type { PrusaConnect, components } from '@prusa/connect';

/** What the signed-in person holds on one device, as the core answers it. */
export type PrincipalGrant = components['schemas']['SubjectHold'];

/**
 * What reading the principal needs of the SDK. A `PrusaConnect` satisfies it,
 * and naming the two operations is what lets a test hand in a fake.
 */
export type PrincipalClient = Pick<
  PrusaConnect,
  'getPrincipal' | 'listSubjectGrants'
>;

/** The signed-in person's id and grants, as a module receives them. */
export interface ModulePrincipal {
  /** The subject the authorization graph is keyed by, such as `user:alice`. */
  readonly id: string;
  /** What the person is called: the account's display name, else its username. Absent for a subject with no account. */
  readonly name?: string | undefined;
  /** The address the identity provider last sent. Absent for a local account. */
  readonly email?: string | undefined;
  /** One entry per device the person holds anything on. Empty when none. */
  readonly grants: readonly PrincipalGrant[];
}

/**
 * Reads the principal through the SDK. Two calls, because the core answers
 * the identity and the holdings at two operations.
 */
export const readPrincipal = async (
  client: PrincipalClient,
): Promise<ModulePrincipal> => {
  const who = await client.getPrincipal();
  const principal = who.principal;
  // The server may hold an empty display name, and an empty name is no name.
  const name = [who.display_name, who.username].find(
    (value) => value !== undefined && value !== '',
  );
  const email = who.email === '' ? undefined : who.email;

  try {
    const held = await client.listSubjectGrants({
      path: { subject: principal },
    });

    return { id: principal, name, email, grants: held.devices ?? [] };
  } catch {
    // A deployment can refuse the grant listing to the person it is about.
    // The identity still stands, so the principal is returned without the
    // holdings rather than not at all.
    return { id: principal, name, email, grants: [] };
  }
};
