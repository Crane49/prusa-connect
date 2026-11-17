// The registration frames as a page reads them: a printer asking to join, the
// decision on it, and a printer turned away at the door. The core publishes
// them on the connector's origin (sdk/event/registration.go), and the payload
// crosses into our code as `unknown`, so every member is read behind a check
// (typescript.md).
import type { EventType, StreamEvent } from '@prusa/connect';

const ANNOUNCED = 'com.prusa.connect.device.registration.announced';
const APPROVED = 'com.prusa.connect.device.registration.approved';
const REJECTED = 'com.prusa.connect.device.registration.rejected';
const REFUSED = 'com.prusa.connect.device.registration.refused';

/**
 * The four registration types. A host adds them to the one stream it opens
 * (ADR-20260911-4p6 rule 23).
 */
export const REGISTRATION_EVENTS: readonly EventType[] = [
  ANNOUNCED,
  APPROVED,
  REJECTED,
  REFUSED,
];

/** Why a connector turned a printer away, as the refused frame names it. */
export type RefusalReason = 'unknown_credentials' | 'not_admitted';

/** What the machine said about itself. Each value is its own spelling. */
interface Described {
  readonly model?: string | undefined;
  readonly firmware?: string | undefined;
}

/** One registration frame, read. */
export type RegistrationChange =
  | (Described & {
      readonly kind: 'announced';
      readonly code: string;
      readonly fingerprint: string;
      readonly serial?: string | undefined;
    })
  | (Described & {
      readonly kind: 'approved';
      readonly code: string;
      /** Empty for an approval that bound no device yet. */
      readonly deviceId?: string | undefined;
      readonly serial?: string | undefined;
    })
  | { readonly kind: 'rejected'; readonly code: string }
  | (Described & {
      readonly kind: 'refused';
      readonly reason: RefusalReason;
      /** Whether that connector asks a person to approve each printer. */
      readonly admission: 'approval' | 'automatic';
      /** The SHA-256 of the identity the printer presented. */
      readonly digest?: string | undefined;
    });

const textIn = (payload: unknown, name: string): string | undefined => {
  if (typeof payload !== 'object' || payload === null) return undefined;
  // The narrowing cast reads one member of a value the server sent, which is
  // a trust boundary (typescript.md).
  const value = (payload as Record<string, unknown>)[name];

  return typeof value === 'string' && value !== '' ? value : undefined;
};

const describedBy = (payload: unknown): Described => ({
  model: textIn(payload, 'model'),
  firmware: textIn(payload, 'firmware'),
});

/**
 * Reads one frame, or answers undefined for a frame that is not a registration
 * or that lacks the member the page acts on.
 */
export const registrationChangeOf = (
  event: StreamEvent,
): RegistrationChange | undefined => {
  const payload = event.payload;

  if (event.type === REFUSED) {
    const reason = textIn(payload, 'reason');
    if (reason !== 'unknown_credentials' && reason !== 'not_admitted') {
      return undefined;
    }

    return {
      kind: 'refused',
      reason,
      admission:
        textIn(payload, 'admission') === 'automatic' ? 'automatic' : 'approval',
      digest: textIn(payload, 'fingerprint_digest'),
      ...describedBy(payload),
    };
  }

  const code = textIn(payload, 'code');
  if (code === undefined) return undefined;

  switch (event.type) {
    case ANNOUNCED:
      return {
        kind: 'announced',
        code,
        fingerprint: textIn(payload, 'fingerprint') ?? '',
        serial: textIn(payload, 'serial'),
        ...describedBy(payload),
      };
    case APPROVED:
      return {
        kind: 'approved',
        code,
        deviceId: textIn(payload, 'device_id'),
        serial: textIn(payload, 'serial'),
        ...describedBy(payload),
      };
    case REJECTED:
      return { kind: 'rejected', code };
    default:
      return undefined;
  }
};
