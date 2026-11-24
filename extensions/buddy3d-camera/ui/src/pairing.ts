// The plain functions of pairing: the archive the camera's SD card holds, the
// name a new camera is offered, and a file handed to the browser to save.
// Nothing here talks to a server.

/**
 * The sdcard_cfg.txt the camera reads: its token, and its Wi-Fi when given.
 * Without them the camera keeps the Wi-Fi it has. The Wi-Fi password is put
 * here in the browser and never sent to a server.
 */
export const sdcardCfg = (ssid: string, password: string, token: string): string =>
  JSON.stringify({ ...(ssid === '' ? {} : { ssid }), ...(password === '' ? {} : { pwd: password }), token });

/** One file in a tar archive. */
export interface TarFile {
  readonly name: string;
  readonly text: string;
}

/**
 * A tar archive of text files, as `tar -cf` writes it. The camera reads
 * local_cfg.tar from its SD card. Each name is under
 * 100 bytes, which the header holds.
 */
export const tar = (files: readonly TarFile[]): Uint8Array<ArrayBuffer> => {
  const encoder = new TextEncoder();
  const blocks: Uint8Array[] = [];
  for (const file of files) {
    const body = encoder.encode(file.text);
    const header = new Uint8Array(512);
    const put = (offset: number, value: string): void => {
      header.set(encoder.encode(value), offset);
    };
    const octal = (value: number, width: number): string =>
      value.toString(8).padStart(width - 1, '0');
    put(0, file.name);
    put(100, octal(0o644, 8));
    put(108, octal(0, 8));
    put(116, octal(0, 8));
    put(124, octal(body.length, 12));
    put(136, octal(Math.floor(Date.now() / 1000), 12));
    put(148, ' '.repeat(8));
    put(156, '0');
    put(257, 'ustar\u000000');
    put(148, `${octal(header.reduce((sum, byte) => sum + byte, 0), 7)}\u0000`);
    blocks.push(header, body, new Uint8Array((512 - (body.length % 512)) % 512));
  }
  blocks.push(new Uint8Array(1024));

  const out = new Uint8Array(blocks.reduce((sum, block) => sum + block.length, 0));
  let at = 0;
  for (const block of blocks) {
    out.set(block, at);
    at += block.length;
  }

  return out;
};

/**
 * The name a new camera on this printer is offered: a DNS label, as the core
 * requires of a resource name, from the printer's id.
 */
export const suggestedName = (deviceId: string): string => {
  const tail = deviceId
    .toLowerCase()
    .replaceAll(/[^a-z0-9]/g, '')
    .slice(-8);

  return tail === '' ? 'buddy3d' : `buddy3d-${tail}`;
};

/** The core's rule for a resource name. */
export const NAME_PATTERN = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

/** Hands the browser a file to save. */
export const saveFile = (name: string, content: BlobPart, type: string): void => {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const link = document.createElement('a');
  link.href = url;
  link.download = name;
  link.click();
  URL.revokeObjectURL(url);
};
