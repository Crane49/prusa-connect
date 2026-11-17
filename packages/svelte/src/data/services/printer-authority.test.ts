import { describe, expect, it } from 'vitest';

import { readPrinterAuthority } from './printer-authority';

const refusedWith = (status: number) => ({
  getPrinterAuthority: () =>
    Promise.reject(Object.assign(new Error(), { status })),
});

describe('readPrinterAuthority', () => {
  it('returns the certificate bytes', async () => {
    const client = {
      getPrinterAuthority: () =>
        Promise.resolve(new Response(new Blob(['DER']))),
    };

    expect(await (await readPrinterAuthority(client))?.text()).toBe('DER');
  });

  it('answers null when the server has none', async () => {
    expect(await readPrinterAuthority(refusedWith(404))).toBeNull();
  });

  it('lets any other refusal through', async () => {
    await expect(readPrinterAuthority(refusedWith(403))).rejects.toMatchObject({
      status: 403,
    });
  });
});
