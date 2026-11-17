/** One folder the picker offers. */
export interface SaveToStorageFolder {
  readonly name: string;
  /** Relative to the storage root, which is what the choice takes. */
  readonly path: string;
}

/** One of this server's storages a file can be kept in. */
export interface SaveToStorageChoice {
  readonly name: string;
  /** What the storage calls itself. */
  readonly label: string;
}

/**
 * Whether a file sent to a printer is kept on this server, and where.
 *
 * Off, the file goes up as a temporary copy that the server deletes once it
 * is on the printer. On, it is saved in the storage and folder chosen here,
 * and stays there like a file uploaded on the Storage page.
 */
export interface SaveToStorageProps {
  keep: boolean;
  onKeepChange: (keep: boolean) => void;
  /** The storages a file can be written to. One storage draws no choice. */
  storages: readonly SaveToStorageChoice[];
  storage: string;
  onStorageChange: (name: string) => void;
  /** The folder inside the storage. The empty string is its root. */
  folder: string;
  onFolderChange: (path: string) => void;
  /** The folder the picker is showing. */
  pickerAt: string;
  /** The folders directly inside `pickerAt`. */
  pickerFolders: readonly SaveToStorageFolder[];
  /** Why the folders inside `pickerAt` could not be listed. */
  pickerFault?: unknown;
  /** Asks the host to list the folders inside another folder. */
  onPickerWalk: (path: string) => void;
}
