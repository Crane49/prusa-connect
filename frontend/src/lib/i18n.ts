// Which language this application starts in, and its own catalogue.
//
// The order is the one `.agents/rules/frontend-i18n.md` rule 8 sets: the
// setting the person chose, then what the browser asks for, then the language
// this deployment configured, then English. The setting is kept in this
// browser's session storage and nowhere else. It is a preference and not a
// fact about the account, so it does not belong in a request, and a person on
// a shared machine does not leave it behind.
//
// `core.frontend.default_language` sits below the browser on purpose: a
// person whose browser asks for a language this app ships reads it in that
// language wherever the server stands.
//
// The application's own texts live under the `app` namespace, registered the
// same way a module's are (ADR-20260911-4p6 rule 18). The shell has few of them: the
// screens are the component library's and their texts come with them.
import {
  locale,
  type LocaleTag,
  localeOf,
  type Messages,
  registerCatalogue,
  SOURCE_LOCALE,
} from '@prusa/connect-ui/i18n';

// Every `<lang>.json` in `../locales`, so a translator adds a language of the
// shell's own texts with one file and no code. A key a file leaves out falls
// back to English (frontend-i18n.md rule 6).
const files = import.meta.glob<Messages>('../locales/*.json', {
  eager: true,
  import: 'default',
});

/** The shell's catalogues by language tag, English among them. */
export const APP_CATALOGUES: ReadonlyMap<string, Messages> = new Map(
  Object.entries(files).map(([path, messages]) => [
    path.slice(path.lastIndexOf('/') + 1, -'.json'.length),
    messages,
  ]),
);

/** The namespace this application's own texts are merged under. */
export const APP_NAMESPACE = 'app';

const SETTING = 'prusa-connect.locale';

/** What the person chose, when this browser remembers a choice. */
const stored = (): string | undefined => {
  try {
    return globalThis.sessionStorage.getItem(SETTING) ?? undefined;
  } catch {
    // A browser with storage switched off is a browser with no setting.
    return undefined;
  }
};

/** Keeps the choice for the next page in this tab. */
const remember = (tag: LocaleTag): void => {
  try {
    globalThis.sessionStorage.setItem(SETTING, tag);
  } catch {
    // Nothing to do: the language is still set for this page.
  }
};

/**
 * The first language this browser asks for that the app has a catalogue for,
 * the stored setting first. `undefined` means this browser has said nothing
 * the app can act on, which is where the deployment's own default belongs.
 */
const asked = (preferred: readonly string[]): LocaleTag | undefined => {
  for (const tag of [stored() ?? '', ...preferred]) {
    const found = localeOf(tag);
    if (found) return found;
  }

  return undefined;
};

// True once the language came from this browser. The server's own default is
// then not applied over it.
let decided = false;

/**
 * Reads the catalogue in, chooses the language, and answers the tag it chose.
 * `main.ts` calls it once before the application mounts.
 */
export const startLocale = (
  preferred: readonly string[] = globalThis.navigator.languages,
): LocaleTag => {
  for (const [language, messages] of APP_CATALOGUES) {
    registerCatalogue(APP_NAMESPACE, language, messages);
  }

  const found = asked(preferred);
  decided = found !== undefined;

  return locale.set(found ?? SOURCE_LOCALE);
};

/**
 * Takes `core.frontend.default_language`. It is the opening language and not
 * a choice, so it is not kept for the next page and it gives way to a browser
 * that already asked for one.
 */
export const applyDefaultLocale = (tag: string): LocaleTag => {
  if (decided) return locale.current;

  return locale.set(tag);
};

/** What the language control does: set it, and keep it for the next page. */
export const chooseLocale = (tag: string): LocaleTag => {
  const chosen = locale.set(tag);
  decided = true;
  remember(chosen);

  return chosen;
};
