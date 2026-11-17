// The file a printer reads from USB. A printer that dials a listener signed by
// this deployment's own authority has to be told to trust connect.der.
import { describe, expect, it } from 'vitest';

import { buildIni } from './presentation';

const connection = {
  host: 'printserver.local',
  port: 8443,
  tls: true,
  wsUrl: 'wss://printserver.local:8443/p/ws',
  registerUrl: 'https://printserver.local:8443/p/register',
};

describe('the printer settings file', () => {
  it('names the server and nothing else a printer has to be told', () => {
    expect(buildIni({ ...connection, tls: false, port: 80 })).toBe(
      [
        '[service::connect]',
        'hostname = printserver.local',
        'port = 80',
        'tls = 0',
      ].join('\n'),
    );
  });

  it('asks the printer to trust connect.der for an authority of our own', () => {
    const ini = buildIni({ ...connection, customCert: true });

    expect(ini).toContain('tls = 1');
    expect(ini).toContain('custom_cert = 1');
  });

  // Loading a file without a token clears the token the printer holds, and
  // the printer gets a new one when its code is approved. A token line, even
  // an empty one, is never what this server wants on a printer.
  it('never carries a token line', () => {
    expect(buildIni(connection)).not.toMatch(/token/);
    expect(buildIni({ ...connection, customCert: true })).not.toMatch(/token/);
  });

  it('says nothing about a custom certificate over plain HTTP or a public one', () => {
    expect(buildIni(connection)).not.toContain('custom_cert');
    expect(
      buildIni({ ...connection, tls: false, customCert: true }),
    ).not.toContain('custom_cert');
  });
});
