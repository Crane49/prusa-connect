import type { HomePreference } from '../../domain';

/** Which page this browser opens on. */
export interface HomeSource {
  /** Where this browser lands, `auto` being "read the printer count". */
  readonly home: HomePreference;
  setHome(next: HomePreference): void;
}

export interface SettingsAppearanceScreenProps {
  home: HomeSource;
  /** False on a server that leaves dashboards out, which hides that choice. */
  dashboards?: boolean;
}
