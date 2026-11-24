import { t } from '@prusa/connect-ui/i18n';
import type { ModuleHost } from '@prusa/connect-ui/slots';
import { describe, expect, it } from 'vitest';

import { faultText } from './faults';
import { filesOn } from './files';

/** A host whose core answers its upload bound, and records what was sent. */
const hostWith = (
  maxUploadBytes: number | undefined,
): { host: ModuleHost; sent: string[] } => {
  const sent: string[] = [];
  const client = {
    getUiSettings: () => Promise.resolve({ max_upload_bytes: maxUploadBytes }),
    uploadStorageObject: (request: { query: { path: string }; body: Blob }) => {
      sent.push(request.query.path);

      return Promise.resolve({ bytes: request.body.size });
    },
  };

  return {
    host: {
      client,
      signal: new AbortController().signal,
    } as unknown as ModuleHost,
    sent,
  };
};

const sized = (bytes: number): Blob => new Blob([new Uint8Array(bytes)]);

describe('filesOn upload', () => {
  // A browser sending a file when the core refuses it often reports a dropped
  // connection, so a file over the bound is refused here, by the core's code.
  it('refuses a file over the server bound without sending it', async () => {
    const { host, sent } = hostWith(8);

    await expect(
      filesOn(host).upload('local', 'big.bgcode', sized(9)),
    ).rejects.toMatchObject({
      code: 'core.validation/UPLOAD_TOO_LARGE',
      params: { limit_bytes: 8 },
    });
    expect(sent).toEqual([]);
  });

  it('sends a file at the bound, and any file when the core names none', async () => {
    const bounded = hostWith(8);
    await filesOn(bounded.host).upload('local', 'at.bgcode', sized(8));
    expect(bounded.sent).toEqual(['at.bgcode']);

    const unbounded = hostWith(undefined);
    await filesOn(unbounded.host).upload('local', 'any.bgcode', sized(64));
    expect(unbounded.sent).toEqual(['any.bgcode']);
  });
});

// The page shows the action line under the title, so the reader learns the
// limit and the key that sets it, not only that the file was refused.
it('names the limit in the text the page shows', async () => {
  const { host } = hostWith(2 * 1024 * 1024);
  const thrown = await filesOn(host)
    .upload('local', 'big.bgcode', sized(3 * 1024 * 1024))
    .catch((error: unknown) => error);

  expect(faultText(thrown, t).action).toContain('2.0 MB');
});

// A failed read of the limit is not kept: the next upload asks again, so the
// page does not go without a check until it is reloaded.
it('asks for the limit again after a read that failed', async () => {
  const sent: string[] = [];
  let reads = 0;
  const client = {
    getUiSettings: () => {
      reads += 1;

      return reads === 1
        ? Promise.reject(new Error('offline'))
        : Promise.resolve({ max_upload_bytes: 8 });
    },
    uploadStorageObject: (request: { query: { path: string }; body: Blob }) => {
      sent.push(request.query.path);

      return Promise.resolve({ bytes: request.body.size });
    },
  };
  const files = filesOn({
    client,
    signal: new AbortController().signal,
  } as unknown as ModuleHost);

  await files.upload('local', 'first.bgcode', sized(64));
  await expect(
    files.upload('local', 'second.bgcode', sized(64)),
  ).rejects.toMatchObject({
    code: 'core.validation/UPLOAD_TOO_LARGE',
  });
  expect(sent).toEqual(['first.bgcode']);
});
