import { describe, expect, it } from 'vitest';

import { downloadDiagnostics, readDiagnosticsExtensions } from './diagnostics';

describe('downloadDiagnostics', () => {
  it('posts the browser buffer and names the file as the server does', async () => {
    let sent: unknown;
    const client = {
      createDiagnosticsBundle: (request: unknown) => {
        sent = request;

        return Promise.resolve(
          new Response(new Blob(['PK']), {
            headers: {
              'content-disposition':
                'attachment; filename="pcl-diagnostics-0.0.1-20261015T120000Z.zip"',
            },
          }),
        );
      },
    };

    const { blob, name } = await downloadDiagnostics(
      client,
      [{ message: 'x' }],
      { modules: true, extensions: true },
    );

    expect(sent).toEqual({
      query: { modules: true, extensions: true },
      body: [{ message: 'x' }],
    });
    expect(name).toBe('pcl-diagnostics-0.0.1-20261015T120000Z.zip');
    expect(await blob.text()).toBe('PK');
  });

  it('sends an empty array and a fallback name when there is nothing else', async () => {
    let sent: unknown;
    const client = {
      createDiagnosticsBundle: (request: unknown) => {
        sent = request;

        return Promise.resolve(new Response(new Blob(['PK'])));
      },
    };

    const { name } = await downloadDiagnostics(client, [], {
      modules: false,
      extensions: false,
    });

    expect(sent).toEqual({
      query: { modules: false, extensions: false },
      body: [],
    });
    expect(name).toBe('pcl-diagnostics.zip');
  });
});

describe('readDiagnosticsExtensions', () => {
  it('answers the titles the server names', async () => {
    const client = {
      getDiagnostics: () => Promise.resolve({ extensions: ['Printer logs'] }),
    };

    expect(await readDiagnosticsExtensions(client)).toEqual(['Printer logs']);
  });
});
