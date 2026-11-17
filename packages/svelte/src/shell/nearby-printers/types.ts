import type { DataFault, NearbyPrinter } from '../../data';

/** The printers this server hears, as the panel reads them. */
export interface NearbySource {
  /** Every printer the server hears, sorted by name. */
  readonly items: readonly NearbyPrinter[];
  /** True once a read has answered, which tells an empty list from an unread one. */
  readonly loaded: boolean;
  /** What the last read was refused with. */
  readonly error: DataFault | undefined;
}

export interface NearbyPrintersPanelProps {
  source: NearbySource;
  /**
   * Where the guide that adds a printer to this server is. Without it a row
   * offers no way to add the printer.
   */
  addHref?: string | undefined;
}
