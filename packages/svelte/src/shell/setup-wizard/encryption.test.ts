// What the encryption step adds to the confirm: the address printers dial,
// which is the web app's own port until encryption gives printers a port of
// their own or the person names one, and the certificate an encrypted printer
// listener carries.
import { describe, expect, it } from 'vitest';

import { encryptionDecision, encryptionErrors } from './encryption';
import { NO_ENCRYPTION } from './setup-draft.svelte';

const made = {
  web: ['/c/web.crt', '/c/web.key'] as [string, string],
  printer: ['/c/printer.crt', '/c/printer.key'] as [string, string],
};

describe('the encryption step', () => {
  it("names the web app's port for printers while both switches are off", () => {
    expect(
      encryptionDecision(
        { ...NO_ENCRYPTION, printerHost: '192.168.1.50' },
        ':8080',
        undefined,
      ),
    ).toEqual({
      answers: {},
      printerAddress: 'http://192.168.1.50:8080',
    });
  });

  it('uses the web port the browser reaches when printers share its listener', () => {
    const unencrypted = {
      ...NO_ENCRYPTION,
      printerHost: '192.168.1.24',
    };
    expect(encryptionErrors(unencrypted, true).host).toBe('');
    expect(
      encryptionDecision(unencrypted, ':8080', '8481').printerAddress,
    ).toBe('http://192.168.1.24:8481');
  });

  it('lets a named port override both the web and printer listener ports', () => {
    const encrypted = {
      ...NO_ENCRYPTION,
      printers: true,
      made,
      printerHost: '192.168.1.24:9443',
    };
    expect(encryptionErrors(encrypted, true).host).toBe('');
    expect(encryptionDecision(encrypted, ':8080', '8481')).toMatchObject({
      printers: { addr: ':8443' },
      printerAddress: 'https://192.168.1.24:9443',
    });
  });

  it("names no address when the operator's file declares how printers connect", () => {
    expect(
      encryptionDecision(
        { ...NO_ENCRYPTION, web: true, made, printerHost: 'printserver.local' },
        undefined,
        undefined,
      ).printerAddress,
    ).toBeUndefined();
  });

  it('refuses a loopback address, which on a printer is the printer itself', () => {
    for (const host of ['localhost', '127.0.0.1', '127.1.2.3', '::1']) {
      expect(
        encryptionErrors({ ...NO_ENCRYPTION, printerHost: host }, true).host,
      ).not.toBe('');
    }
    expect(
      encryptionErrors({ ...NO_ENCRYPTION, printerHost: '192.168.1.50' }, true)
        .host,
    ).toBe('');
    expect(
      encryptionErrors({ ...NO_ENCRYPTION, printerHost: '' }, false).host,
    ).toBe('');
  });

  it('refuses a host that carries a scheme, a path or more than a printer keeps', () => {
    for (const host of [
      'http://x',
      'http://192.168.1.50:8080',
      '192.168.1.50/p',
      '192.168.1.50:0',
      '192.168.1.50:65536',
      ':8080',
      'localhost:8080',
      'fe80::1',
      '[fe80::1]',
      '[fe80::1]:8481',
      'a-very-long-printserver.example',
      '0.0.0.0',
    ]) {
      expect(
        encryptionErrors({ ...NO_ENCRYPTION, printerHost: host }, true).host,
      ).not.toBe('');
    }
    for (const host of [
      'printserver.local',
      'b.cl.prusa3d.com',
      '192.168.1.50:8481',
    ]) {
      expect(
        encryptionErrors({ ...NO_ENCRYPTION, printerHost: host }, true).host,
      ).toBe('');
    }
  });

  it('gives printers a plain port of their own once the web app is encrypted', () => {
    const decision = encryptionDecision(
      {
        ...NO_ENCRYPTION,
        web: true,
        made,
        printerHost: 'printserver.local',
      },
      ':8080',
      '8481',
    );

    expect(decision.answers).toEqual({
      'core.api.tls.certificate': '/c/web.crt',
      'core.api.tls.key': '/c/web.key',
    });
    expect(decision.printers).toEqual({ addr: ':8443' });
    expect(decision.printerAddress).toBe('http://printserver.local:8443');
  });

  it('encrypts the printers with the certificate the server made', () => {
    const decision = encryptionDecision(
      {
        ...NO_ENCRYPTION,
        printers: true,
        made,
        printerHost: '192.168.1.50',
      },
      ':8080',
      undefined,
    );

    expect(decision.printers).toEqual({
      addr: ':8443',
      certificate: '/c/printer.crt',
      key: '/c/printer.key',
    });
    expect(decision.printerAddress).toBe('https://192.168.1.50:8443');
  });

  it('asks for certificates before it lets the person on', () => {
    const errors = encryptionErrors(
      {
        ...NO_ENCRYPTION,
        web: true,
        printerHost: 'x',
      },
      true,
    );

    expect(errors.web).not.toBe('');
    expect(
      encryptionErrors(
        { ...NO_ENCRYPTION, web: true, made, printerHost: 'x' },
        true,
      ).web,
    ).toBe('');
  });

  it('refuses a port written without its colon', () => {
    const errors = encryptionErrors(
      {
        ...NO_ENCRYPTION,
        printers: true,
        made,
        printerPort: '8443',
        printerHost: 'x',
      },
      true,
    );

    expect(errors.port).not.toBe('');
  });
});
