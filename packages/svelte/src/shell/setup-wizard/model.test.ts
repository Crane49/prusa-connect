// The order of the steps, which keys a path asks, and what the confirm sends.
import { describe, expect, it } from 'vitest';

import type { SetupConfiguration, SetupKey } from '../../data';
import {
  configurationOf,
  isActive,
  keysOf,
  movedOrigin,
  originAfter,
  reachedOf,
  settleLocked,
  stepsOf,
} from './model';

const key = (path: string, extra: Partial<SetupKey> = {}): SetupKey => ({
  path,
  group: path.split('.')[1] ?? 'server',
  level: 'advanced',
  kind: 'text',
  default: null,
  choices: [],
  doc: '',
  secret: false,
  locked: false,
  reference: `config/#key-${path}`,
  ...extra,
});

const contract = (keys: SetupKey[], declared = false): SetupConfiguration => ({
  config_file: { path: '/srv/config/config.yaml', exists: false },
  setup_file: { path: '/srv/config/config.d/setup.yaml', exists: false },
  keys,
  storage: { root: '/srv/storage', declared },
  hosts: [],
  printers: true,
  printers_tls: false,
});

const KEYS = [
  key('core.api.addr', { level: 'basic', default: ':8080' }),
  key('core.api.tls.certificate'),
  key('core.discovery.enabled', {
    kind: 'switch',
    level: 'basic',
    default: true,
    group: 'discovery',
  }),
  key('core.discovery.hidden', {
    kind: 'switch',
    level: 'basic',
    default: false,
    group: 'discovery',
    depends_on: 'core.discovery.enabled',
  }),
  key('persistence.type', {
    group: 'database',
    default: 'sqlite',
    choices: ['sqlite', 'postgres'],
  }),
  key('persistence.postgres.dsn', {
    group: 'database',
    depends_on: 'persistence.type=postgres',
  }),
];

describe('the steps', () => {
  it('knows only the first two until the code is accepted', () => {
    expect(stepsOf(undefined, undefined)).toEqual(['welcome', 'code']);
  });

  it('asks fewer sections on the basic path than on the advanced one', () => {
    const basic = stepsOf(contract(KEYS), 'basic');
    const advanced = stepsOf(contract(KEYS), 'advanced');

    expect(basic).toContain('section:network');
    expect(basic).not.toContain('section:database');
    expect(advanced).toContain('section:database');
    expect(advanced.at(-1)).toBe('summary');
  });

  it('keeps the storage step when the file declares a storage, for the history', () => {
    expect(stepsOf(contract(KEYS, true), 'basic')).toContain('storage');
  });

  it('draws only the basic keys on the basic path', () => {
    expect(
      keysOf(contract(KEYS), 'network', 'basic').map((each) => each.path),
    ).toEqual([
      'core.api.addr',
      'core.discovery.enabled',
      'core.discovery.hidden',
    ]);
  });
});

describe('the steps a person may open again', () => {
  const basic = stepsOf(contract(KEYS), 'basic');

  it('reaches the summary once every step before it was seen', () => {
    expect(reachedOf(basic, basic, 2)).toBe(basic.length - 1);
  });

  it('stops before the first step a new path adds', () => {
    const advanced = stepsOf(contract(KEYS), 'advanced');
    const added = advanced.indexOf('section:database');

    expect(reachedOf(advanced, basic, 2)).toBe(added - 1);
  });

  it('always reaches the step the person is on', () => {
    expect(reachedOf(basic, [], 3)).toBe(3);
  });
});

describe('a key under a switch', () => {
  it('is on while its switch is on, and off with it', () => {
    const hidden = KEYS.find((each) => each.path === 'core.discovery.hidden');
    if (hidden === undefined) throw new Error('no such key');

    expect(isActive(hidden, contract(KEYS), {})).toBe(true);
    expect(
      isActive(hidden, contract(KEYS), { 'core.discovery.enabled': false }),
    ).toBe(false);
  });

  it('follows the value of a closed set', () => {
    const dsn = KEYS.find((each) => each.path === 'persistence.postgres.dsn');
    if (dsn === undefined) throw new Error('no such key');

    expect(isActive(dsn, contract(KEYS), {})).toBe(false);
    expect(
      isActive(dsn, contract(KEYS), { 'persistence.type': 'postgres' }),
    ).toBe(true);
  });

  it('is not sent while its switch is off', () => {
    const sent = configurationOf(contract(KEYS), {
      'core.discovery.enabled': false,
      'core.discovery.hidden': true,
    });

    expect(sent).toEqual({ 'core.discovery.enabled': false });
  });
});

describe('the access section', () => {
  const ACCESS = contract([
    key('core.auth.local', { kind: 'switch', default: false }),
    key('core.auth.password_passes', { kind: 'number', default: 3 }),
    key('core.authz.org_admin_handover', { kind: 'switch', default: false }),
  ]);
  const paths = (keys: SetupKey[]) => keys.map((one) => one.path);

  it('never draws the password login, which the identity step decides', () => {
    expect(paths(keysOf(ACCESS, 'access', 'advanced', 'required'))).toEqual([
      'core.auth.password_passes',
      'core.authz.org_admin_handover',
    ]);
    expect(
      configurationOf(ACCESS, { 'core.auth.local': true }, 'required'),
    ).toEqual({});
  });

  it('asks the cost of a password only when people sign in with one', () => {
    for (const mode of ['open', 'oidc'] as const) {
      expect(paths(keysOf(ACCESS, 'access', 'advanced', mode))).toEqual([
        'core.authz.org_admin_handover',
      ]);
      expect(
        configurationOf(ACCESS, { 'core.auth.password_passes': '5' }, mode),
      ).toEqual({});
    }
    expect(
      configurationOf(ACCESS, { 'core.auth.password_passes': '5' }, 'required'),
    ).toEqual({ 'core.auth.password_passes': 5 });
  });
});

describe('the confirm', () => {
  it('sends only what differs from where each key started', () => {
    expect(
      configurationOf(contract(KEYS), { 'core.api.addr': ':8080' }),
    ).toEqual({});
    expect(
      configurationOf(contract(KEYS), { 'core.api.addr': ':9090' }),
    ).toEqual({
      'core.api.addr': ':9090',
    });
  });

  it('works out the address the server moves to', () => {
    const here = new URL('http://192.168.1.10:8080/');

    expect(originAfter(contract(KEYS), {}, here)).toBe(
      'http://192.168.1.10:8080',
    );
    expect(
      originAfter(contract(KEYS), { 'core.api.addr': ':9090' }, here),
    ).toBe('http://192.168.1.10:9090');
    expect(
      originAfter(
        contract(KEYS),
        { 'core.api.addr': ':8443', 'core.api.tls.certificate': '/c.pem' },
        here,
      ),
    ).toBe('https://192.168.1.10:8443');
  });

  it('stays on a published port the server does not listen on', () => {
    const published = new URL('http://192.168.1.10:18080/');

    expect(movedOrigin(contract(KEYS), {}, published)).toBeUndefined();
    expect(
      movedOrigin(contract(KEYS), { 'core.api.addr': ':8080' }, published),
    ).toBeUndefined();
    expect(
      movedOrigin(contract(KEYS), { 'core.api.addr': ':9090' }, published),
    ).toBe('http://192.168.1.10:9090');
  });

  it('moves when the answers change the listener', () => {
    const here = new URL('http://192.168.1.10:8080/');

    expect(movedOrigin(contract(KEYS), {}, here)).toBeUndefined();
    expect(
      movedOrigin(contract(KEYS), { 'core.api.addr': ':9090' }, here),
    ).toBe('http://192.168.1.10:9090');
  });
});

describe('settleLocked', () => {
  // A step that writes its key itself, such as the printers' address, must
  // not send a key the operator's file sets: the server refuses the whole
  // confirm for one such key.
  it('drops a key the file sets to the same value, and tells nobody', () => {
    const held = contract([
      key('core.frontend.printer_address', {
        locked: true,
        value: 'http://192.168.1.5:8080',
      }),
      key('core.api.request_timeout', {
        locked: true,
        kind: 'number',
        value: 30,
      }),
    ]);

    expect(
      settleLocked(held, {
        'core.frontend.printer_address': 'http://192.168.1.5:8080',
        'core.api.request_timeout': 30,
        'core.frontend.printer_custom_ca': true,
      }),
    ).toEqual({
      configuration: { 'core.frontend.printer_custom_ca': true },
      kept: [],
      blocked: [],
    });
  });

  it('drops a key the file sets to another value, and names it', () => {
    const address = key('core.frontend.printer_address', {
      locked: true,
      value: 'http://printers.lan:8080',
    });

    const settled = settleLocked(contract([address]), {
      'core.frontend.printer_address': 'http://192.168.1.5:8080',
      'core.log.level': 'debug',
    });

    expect(settled.configuration).toEqual({ 'core.log.level': 'debug' });
    expect(settled.kept.map((each) => each.path)).toEqual([
      'core.frontend.printer_address',
    ]);
    expect(settled.blocked).toEqual([]);
  });

  // An issuer from the file with a client from the wizard is a sign-in
  // provider nobody configured.
  it('blocks the sign-in keys when the file sets one to another value', () => {
    const held = contract([
      key('core.auth.issuer', {
        locked: true,
        value: 'https://id.example.com',
      }),
      key('core.auth.client_id'),
      key('core.auth.client_secret', { secret: true }),
      key('core.principal'),
    ]);

    const settled = settleLocked(held, {
      'core.auth.issuer': 'https://other.example.com',
      'core.auth.client_id': 'connect',
      'core.auth.client_secret': 's3cret',
      'core.principal': 'owner',
      'core.log.level': 'debug',
    });

    expect(settled.configuration).not.toHaveProperty('core.auth.issuer');
    expect(settled.kept.map((each) => each.path)).toEqual(['core.auth.issuer']);
    expect(settled.blocked).toEqual(['identity']);
  });

  it('keeps the rest of the sign-in keys when the file sets one to the same value', () => {
    const held = contract([
      key('core.auth.issuer', {
        locked: true,
        value: 'https://id.example.com',
      }),
      key('core.auth.client_id'),
    ]);

    expect(
      settleLocked(held, {
        'core.auth.issuer': 'https://id.example.com',
        'core.auth.client_id': 'connect',
      }),
    ).toEqual({
      configuration: { 'core.auth.client_id': 'connect' },
      kept: [],
      blocked: [],
    });
  });

  // The server never shows a secret back, so the wizard cannot tell whether
  // its value is the file's. The file's secret wins, and the rest still goes.
  it("keeps the file's secret without blocking the sign-in keys", () => {
    const settled = settleLocked(
      contract([
        key('core.auth.client_secret', { locked: true, secret: true }),
        key('core.auth.client_id'),
        key('core.principal'),
      ]),
      {
        'core.auth.client_secret': 's3cret',
        'core.auth.client_id': 'connect',
        'core.principal': 'owner',
      },
    );

    expect(settled.configuration).toEqual({
      'core.auth.client_id': 'connect',
      'core.principal': 'owner',
    });
    expect(settled.kept.map((each) => each.path)).toEqual([
      'core.auth.client_secret',
    ]);
    expect(settled.blocked).toEqual([]);
  });

  // The wizard fills the redirect address in itself, from this page.
  it("keeps the file's redirect address without blocking the sign-in keys", () => {
    const settled = settleLocked(
      contract([
        key('core.auth.redirect_uri', {
          locked: true,
          value: 'https://connect.example.com/api/v1/auth/callback',
        }),
        key('core.auth.client_id'),
      ]),
      {
        'core.auth.redirect_uri':
          'http://192.168.1.5:8080/api/v1/auth/callback',
        'core.auth.client_id': 'connect',
      },
    );

    expect(settled.configuration).toEqual({ 'core.auth.client_id': 'connect' });
    expect(settled.blocked).toEqual([]);
  });

  it('blocks the certificate keys when the file sets the certificate', () => {
    const settled = settleLocked(
      contract([
        key('core.api.tls.certificate', {
          locked: true,
          value: '/etc/tls/api.crt',
        }),
        key('core.api.tls.key'),
      ]),
      {
        'core.api.tls.certificate': 'certs/api.crt',
        'core.api.tls.key': 'certs/api.key',
        'core.frontend.printer_custom_ca': true,
      },
    );

    expect(settled.configuration).not.toHaveProperty(
      'core.api.tls.certificate',
    );
    expect(settled.kept.map((each) => each.path)).toEqual([
      'core.api.tls.certificate',
    ]);
    expect(settled.blocked).toEqual(['encryption']);
  });
});
