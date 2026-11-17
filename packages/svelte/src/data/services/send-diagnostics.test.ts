import { describe, expect, it } from 'vitest';

import {
  DiagnosticsSendError,
  diagnosticsUploadUrl,
  sendDiagnostics,
} from './send-diagnostics';

const answer = (status: number, body: unknown, headers = {}): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  });

describe('sendDiagnostics', () => {
  it('posts the zip with no credentials and answers the portal page', async () => {
    let sent: { url: string; init: RequestInit } | undefined;
    const bundle = new Blob(['PK'], { type: 'application/zip' });

    const upload = await sendDiagnostics(
      bundle,
      'https://beta.example/',
      '0.0.1-alpha.4',
      (url, init) => {
        sent = { url, init };

        return Promise.resolve(
          answer(201, {
            url: 'https://beta.example/prusa-connect/diagnostics/abc',
            expiresAt: '2026-10-19T12:00:00Z',
          }),
        );
      },
    );

    expect(sent?.url).toBe(
      'https://beta.example/api/diagnostics?product=connect-local&version=0.0.1-alpha.4',
    );
    expect(sent?.init).toEqual({
      method: 'POST',
      headers: { 'Content-Type': 'application/zip' },
      body: bundle,
      credentials: 'omit',
    });
    expect(upload).toEqual({
      url: 'https://beta.example/prusa-connect/diagnostics/abc',
      expiresAt: '2026-10-19T12:00:00Z',
    });
  });

  it('keeps the status, the code, the message and the wait of a refusal', async () => {
    const refused = sendDiagnostics(
      new Blob(['PK']),
      'https://beta.example',
      '1',
      () =>
        Promise.resolve(
          answer(
            429,
            { error: 'rate_limited', message: 'Too many uploads' },
            { 'retry-after': '120' },
          ),
        ),
    );

    await expect(refused).rejects.toBeInstanceOf(DiagnosticsSendError);
    await expect(refused).rejects.toMatchObject({
      status: 429,
      code: 'rate_limited',
      message: 'Too many uploads',
      retryAfter: 120,
    });
  });

  it('reports a portal it could not reach as status 0', async () => {
    const refused = sendDiagnostics(
      new Blob(['PK']),
      'https://beta.example',
      '1',
      () => Promise.reject(new TypeError('Failed to fetch')),
    );

    await expect(refused).rejects.toMatchObject({
      status: 0,
      message: 'Failed to fetch',
    });
  });

  it('refuses a page outside the portal', async () => {
    const refused = sendDiagnostics(
      new Blob(['PK']),
      'https://beta.example',
      '1',
      () =>
        Promise.resolve(
          answer(201, { url: 'javascript:alert(1)', expiresAt: '' }),
        ),
    );

    await expect(refused).rejects.toBeInstanceOf(DiagnosticsSendError);
  });
});

describe('diagnosticsUploadUrl', () => {
  it('keeps a path the portal is served under', () => {
    expect(diagnosticsUploadUrl('https://support.example/beta', '1.0')).toBe(
      'https://support.example/beta/api/diagnostics?product=connect-local&version=1.0',
    );
  });
});
