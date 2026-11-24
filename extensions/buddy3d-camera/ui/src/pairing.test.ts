import { describe, expect, it } from 'vitest';

import { NAME_PATTERN, sdcardCfg, suggestedName, tar } from './pairing';

describe('pairing', () => {
  it('carries the Wi-Fi and the token in the shape the firmware reads', () => {
    expect(JSON.parse(sdcardCfg('Workshop', 'pa"ss', '0123456789abcdefghij'))).toEqual({
      ssid: 'Workshop',
      pwd: 'pa"ss',
      token: '0123456789abcdefghij',
    });
    expect(JSON.parse(sdcardCfg('', '', '0123456789abcdefghij'))).toEqual({ token: '0123456789abcdefghij' });
    expect(JSON.parse(sdcardCfg('Open', '', 't'))).toEqual({ ssid: 'Open', token: 't' });
  });

  it('packs files the way tar -cf does', () => {
    const archive = tar([
      { name: 'srv_cfg.txt', text: 'NTP_SERVER=ntp.lan\n' },
      { name: 'b', text: 'x'.repeat(513) },
    ]);
    const header = archive.subarray(0, 512);
    const field = (from: number, to: number): string =>
      new TextDecoder().decode(header.subarray(from, to)).replace(/[\0 ]+$/, '');
    // Two files of one and two blocks, each behind its header, then two empty blocks.
    expect(archive.length).toBe(512 * (2 + 1 + 2 + 2));
    expect(field(0, 100)).toBe('srv_cfg.txt');
    expect(parseInt(field(124, 136), 8)).toBe(19);
    expect(field(257, 263)).toBe('ustar');
    const sum = header.reduce((total, byte, i) => total + (i >= 148 && i < 156 ? 32 : byte), 0);
    expect(parseInt(field(148, 156), 8)).toBe(sum);
    expect(new TextDecoder().decode(archive.subarray(512, 531))).toBe('NTP_SERVER=ntp.lan\n');
  });

  it('offers a name the core takes', () => {
    for (const id of ['pcore0000000000000000001', '7547B720-CE5D-4294', '', '---']) {
      expect(suggestedName(id)).toMatch(NAME_PATTERN);
    }
  });
});
