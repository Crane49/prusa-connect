// The address a printer is told to dial, as the setup wizard asks it and the
// add-printer page writes it into prusa_printer_settings.ini.

const OWN = ['localhost', '127.0.0.1', '[::1]', '::1', '0.0.0.0', '::', '[::]'];

/**
 * Whether a host names only the machine that dials it: a loopback name, or
 * the unspecified address. On a printer that is the printer itself, so it is
 * never the address a printer is given.
 */
export const isOwnAddress = (host: string): boolean =>
  OWN.includes(host.trim().toLowerCase()) ||
  /^127\.\d+\.\d+\.\d+$/.test(host.trim());
