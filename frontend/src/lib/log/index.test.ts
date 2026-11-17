import { describe, expect, it } from 'vitest';

import { entries, log, navigate, redact } from './index';

describe('log', () => {
  it('keeps info and above in the ring, newest last, at most 200', () => {
    const scoped = log.scope('test');
    for (let i = 0; i < 205; i += 1) scoped.info('tick', { i });
    scoped.debug('not kept');

    const kept = entries();
    expect(kept).toHaveLength(200);
    expect(kept.at(-1)?.fields).toEqual({ i: 204 });
    expect(kept.at(-200)?.fields).toEqual({ i: 5 });
    expect(kept.at(-1)?.scope).toBe('test');
  });

  it('scrubs the message of an entry the ring keeps', () => {
    log.scope('msg').info('sent to jan@example.cz');

    expect(entries().at(-1)?.message).toBe('sent to <email>');
  });

  it('evaluates lazy fields only once', () => {
    let calls = 0;
    log.scope('lazy').warn('w', () => {
      calls += 1;

      return { n: 1 };
    });
    expect(calls).toBe(1);
  });
});

describe('redact', () => {
  it('drops sensitive keys and scrubs strings', () => {
    expect(
      redact({
        password: 'x',
        fileName: 'a.gcode',
        path: '/x',
        userEmail: 'a@b.cz',
        status: 500,
        error:
          'GET https://u:p@pcl.lan/api/v1/x?token=1 from 10.0.0.5 by jan@example.cz',
      }),
    ).toEqual({ status: 500, error: 'GET <url> from <ip> by <email>' });
  });
});

describe('redact, the patterns the server also applies', () => {
  const text = (value: string): unknown => redact({ error: value }).error;

  it.each([
    ['password=hunter22&next', 'password=<redacted>&next'],
    ['{"api_key": "abc 123"}', '{"api_key": <redacted>}'],
    ["access_token: 'x y'", 'access_token: <redacted>'],
    ['Authorization: Basic dXNlcjpwYXNz', 'Authorization: <credentials>'],
    ['"authorization": "Digest u=1"', '"authorization": "<credentials>"'],
    ['Cookie: sid=abc; theme=dark', 'Cookie: <redacted>'],
    ['"set-cookie": ["sid=abc"]', '"set-cookie": ["<redacted>"]'],
    ['{"Authorization":["Basic dXNl"]}', '{"Authorization":["<credentials>"]}'],
    ['key 3fA9kQ2mZ7xW1pL8vB4nR6tY0cH5jD2s', 'key <redacted>'],
    ['at /Users/Jan Novak/prints/a.gcode', 'at <path>'],
    ['C:\\Users\\jan\\a.gcode failed', '<path> failed'],
    ['read /var/lib/pcl/x.db', 'read <path>'],
    ['GET /api/v1/devices/1 failed', 'GET /api/v1/devices/1 failed'],
    ['from fe80::1 and [2001:db8::2]:443', 'from <ip> and <ip>'],
    ['lookup mqtt.home.lan on <ip>', 'lookup <host> on <ip>'],
    ['dial tcp broker.example.net:8883', 'dial tcp <host>:8883'],
    ['answering for workshop.local', 'answering for <host>'],
    ['failed for user "lukas"', 'failed for user <user>'],
    ['printer 00:1A:2b:3C:4d:5E joined', 'printer <mac> joined'],
  ])('%s', (input, want) => {
    expect(text(input)).toBe(want);
  });

  it.each([
    'at 12:34:56 it stopped',
    'panic at main.go:42',
    'device 34e039fc-13fd-4f4a-b16b-92392643f63a',
    'trace 4bf92f3577b34da6a3ce929d0e0e4736',
    'revision=0123456789abcdef0123456789abcdef01234567',
    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  ])('keeps %s', (input) => {
    expect(text(input)).toBe(input);
  });

  it('scrubs nested fields and shares nothing with the caller', () => {
    const inner = { detail: 'mail jan@example.cz', token: 'x' };
    const fields = { context: inner, list: ['10.0.0.5', { password: 'y' }] };

    const out = redact(fields);

    expect(out).toEqual({
      context: { detail: 'mail <email>' },
      list: ['<ip>', {}],
    });
    expect(out.context).not.toBe(inner);
    expect(inner.detail).toBe('mail jan@example.cz');
  });

  it('keeps an Error as its text', () => {
    expect(redact({ cause: new Error('at 10.0.0.5') })).toEqual({
      cause: 'Error: at <ip>',
    });
  });
});

describe('navigate', () => {
  it('records the route template and stamps it on every later entry', () => {
    navigate('/printers/:id');
    log.scope('later').error('failed');

    const [nav, later] = entries().slice(-2);
    expect(nav).toMatchObject({
      scope: 'nav',
      level: 'info',
      message: 'navigate',
      route: '/printers/:id',
      fields: { route: '/printers/:id' },
    });
    expect(later?.route).toBe('/printers/:id');
  });

  it('leaves the route out where no pattern answered', () => {
    navigate(undefined);
    log.scope('later').error('failed');

    expect(entries().at(-1)).not.toHaveProperty('route');
  });
});
