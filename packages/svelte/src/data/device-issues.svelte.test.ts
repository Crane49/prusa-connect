import { describe, expect, it } from 'vitest';

import { DeviceIssues } from './device-issues.svelte';
import type { DeviceIssuesClient } from './services/device-issues';

const stub = (fail = false) => {
  let calls = 0;
  const client: DeviceIssuesClient = {
    getDeviceIssueVocabulary: () => {
      calls += 1;
      if (fail) return Promise.reject(new Error('down'));

      return Promise.resolve({
        format: 'device-issues',
        items: [
          {
            action: 'Load filament.',
            description: '',
            reason: 'filament_runout',
            severity: 'warning',
            summary: 'The filament ran out.',
          },
        ],
        severities: [],
        version: '',
        vocabulary: 'device-issues',
      });
    },
  };

  return { client, calls: () => calls };
};

describe('DeviceIssues', () => {
  it('answers the entry of a listed reason and nothing for another', async () => {
    const issues = new DeviceIssues(stub().client);
    await issues.read();

    expect(issues.entryOf('filament_runout')?.summary).toBe(
      'The filament ran out.',
    );
    expect(issues.entryOf('unheard_of')).toBeUndefined();
  });

  it('reads once however often it is asked', async () => {
    const { client, calls } = stub();
    const issues = new DeviceIssues(client);

    await Promise.all([issues.read(), issues.read()]);
    await issues.read();

    expect(calls()).toBe(1);
  });

  it('keeps the fault and asks again after a failed read', async () => {
    const { client, calls } = stub(true);
    const issues = new DeviceIssues(client);
    await issues.read();

    expect(issues.error?.message).toBe('down');
    expect(issues.entryOf('filament_runout')).toBeUndefined();

    await issues.read();
    expect(calls()).toBe(2);
  });
});
