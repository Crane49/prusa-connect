import { describe, expect, it } from 'vitest';
import { describeFault } from '../i18n/fault';
import { faultOf } from './fault';

describe('faultOf', () => {
  it('keeps the trace id the server stamped on a refusal', () => {
    const refusal = Object.assign(new Error('boom'), {
      code: 'core.x/Y',
      status: 500,
      traceId: 'abc123',
    });

    const fault = faultOf(refusal);

    expect(fault.traceId).toBe('abc123');
    expect(describeFault(fault).traceId).toBe('abc123');
  });

  it('has no trace id for a failure that never reached the server', () => {
    expect(faultOf(new Error('offline')).traceId).toBeUndefined();
  });
});
