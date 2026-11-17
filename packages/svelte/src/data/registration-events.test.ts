import type { StreamEvent } from '@prusa/connect';
import { describe, expect, it } from 'vitest';

import { registrationChangeOf } from './registration-events';

const frame = (kind: string, payload: unknown): StreamEvent =>
  // The cast stands in for the narrowed union the SDK yields for one type.
  ({
    type: `com.prusa.connect.device.registration.${kind}`,
    id: '1',
    payload,
    envelope: undefined,
  }) as unknown as StreamEvent;

describe('registrationChangeOf', () => {
  it('reads the four frames the core publishes', () => {
    expect(
      registrationChangeOf(
        frame('announced', {
          code: 'A1',
          fingerprint: 'fp',
          serial: 'S',
          model: 'MK4',
        }),
      ),
    ).toEqual({
      kind: 'announced',
      code: 'A1',
      fingerprint: 'fp',
      serial: 'S',
      model: 'MK4',
      firmware: undefined,
    });
    expect(
      registrationChangeOf(frame('approved', { code: 'A1', device_id: 'd' })),
    ).toMatchObject({ kind: 'approved', code: 'A1', deviceId: 'd' });
    expect(registrationChangeOf(frame('rejected', { code: 'A1' }))).toEqual({
      kind: 'rejected',
      code: 'A1',
    });
    expect(
      registrationChangeOf(
        frame('refused', {
          reason: 'not_admitted',
          admission: 'automatic',
          fingerprint_digest: 'ab',
        }),
      ),
    ).toEqual({
      kind: 'refused',
      reason: 'not_admitted',
      admission: 'automatic',
      digest: 'ab',
      model: undefined,
      firmware: undefined,
    });
  });

  it('answers nothing for a frame it cannot act on', () => {
    expect(
      registrationChangeOf(frame('announced', { fingerprint: 'fp' })),
    ).toBe(undefined);
    expect(registrationChangeOf(frame('refused', { reason: 'other' }))).toBe(
      undefined,
    );
    expect(registrationChangeOf(frame('approved', 'not an object'))).toBe(
      undefined,
    );
    expect(
      registrationChangeOf({
        type: 'com.prusa.connect.device.arrived',
        id: '1',
        payload: { code: 'A1' },
        envelope: undefined,
      } as unknown as StreamEvent),
    ).toBe(undefined);
  });
});
