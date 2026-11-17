---
paths:
  - "**/*.svelte"
  - "**/*.svelte.ts"
  - "**/locales/*.json"
---

# Frontend texts and languages

Every text a person reads is a key in a catalogue. A catalogue is a file that
maps each key to the sentence shown in one language, and English is the
source catalogue. The frontend is read in Czech, German, French, Spanish,
Italian and Polish as well as English, and the community adds more. A fault
is shown by its code, with the server's English message kept beside it.

A module's bundle arrives at run time from another repository, so a catalogue
is read at run time too. Nothing here can be decided at compile time, because
the compiler never sees a module's strings.

The code is `@prusa/connect-ui/i18n`. The gate, the check that refuses a
change, is `frontend-standards/no-literal-text` in the ESLint config of the
library, the application and a module's own tree, plus
`packages/svelte/src/i18n/catalogue.test.ts`.

## The rules

1. No English string in a component. Not in the markup between two tags,
   and not in `title`, `aria-label`, `placeholder` or `alt`. Every text is
   `t('key')`. A store, a service and a variants file hold keys, never
   sentences.
2. A key says where the text is used, never what it says. `fleet.empty`,
   `device.session.gone`, `signIn.password`. A reworded message keeps its key
   and keeps every translation with it. Dotted, lower camel case after the
   first part, and the first part names the screen or the component.
3. Numbers, dates, units and plurals go through `Intl`, the browser's own
   formatting library. `formatNumber`, `formatDate`, `formatDateTime`,
   `formatRelative`, `formatBytes`, `formatDuration`, `formatList` and the
   `{count, plural, one {…} other {…}}` form of a message, which reads
   `Intl.PluralRules`. Never `toFixed` plus a unit, never
   `count === 1 ? … : …`, never a hand-written "or". Czech has four plural
   categories and Polish three, and a comparison against one is wrong in both.
4. Parameters are named. `t('device.printing', { name })`, never a
   positional slot and never a sentence glued together from pieces. The order
   of the parts of a sentence belongs to the language.
5. A fault renders by its code. `describeFault(thrown)` answers
   `{ code, title, detail }`. The title is `error.<code>` from the catalogue,
   or the server's English `message` when nothing is translated. The English
   message is shown under it as the detail, and the code beside it. A store
   keeps the fault it caught and never flattens it to a string: the code is
   what a translation is keyed by, and a message loses it.
6. A key missing from a language falls back to English, never to the key.
   A translator leaves out what they have not translated. A key no catalogue
   holds at all is a fault: it is recorded on `locale.missing` and the gate
   fails.
7. A module ships `locales/<lang>.json` beside its entry. The host reads
   `locales/en.json` and the file for the language on screen. It merges them
   under the module's namespace and hands the bundle a `t` bound to it, as
   `host.t`. A module that ships none shows the English its code carries, and
   absence is never a fault. A module may name a key the core already names,
   and the module's own wins.
8. The language comes from the person, then the browser, then English. The
   setting the person chose, held in this browser's session storage; then
   `navigator.languages`; then English. It is set once before the first screen
   draws, so nothing flashes in one language and is redrawn in another.
9. English is generated into the code, and the gate keeps it honest.
   `packages/svelte/locales/*.json` are the files a translator edits.
   `pnpm build` runs `scripts/emit-catalogues.mjs`. It writes
   `locales/en.errors.json` from `docs/errors/error-codes.json`, and
   `src/i18n/catalogues.ts` from every locale file.
   `make generate-printer-error-codes` writes `locales/en.printerCode.json`
   from Prusa-Error-Codes, and `describePrinterCode(code)` reads it. The answers under
   a printer's question are in `locales/en.printerButton.json`, which a person
   edits, and `printerButtonLabel(name)` reads them. Both are the words of
   the xbuddy driver, and the dialog reads them for its printers alone. The test fails on a key in
   use that English lacks and on a key in English that nothing uses. It also
   fails on a generated module older than the files it came from.
10. What is not a text. A brand name, a material name, a unit symbol and a
    wire value are the same in every language and stay as they are. The gate
    carries the allow list; add to it instead of keying a word that has no
    translation.
11. English is split by screen, the other languages are not. A screen writes
    `locales/en.<screen>.json` and owns one key prefix; the generator merges
    every file whose name starts `en.` into the one `en` catalogue, so two
    screens are never edited in one file. A translator still takes one file per
    language. Two files holding the same key merge silently, last by filename,
    so the duplicate gate in `src/i18n/catalogue.test.ts` is what makes the
    split safe.

## The shape

```svelte
<script lang="ts">
  import { describeFault, formatBytes, t } from '@prusa/connect-ui/i18n';

  let { file, error }: Props = $props();

  const fault = $derived(error ? describeFault(error) : undefined);
</script>

<h1>{t('files.title')}</h1>
<p>{t('files.count', { count: files.length })}</p>
<span>{formatBytes(file.size)}</span>

{#if fault}
  <p role="alert">
    {fault.title}
    {#if fault.detail}<span class="block text-xs">{fault.detail}</span>{/if}
    {#if fault.code}<code class="block text-xs">{fault.code}</code>{/if}
  </p>
{/if}
```

A default a caller may replace reads the catalogue in a `$derived`, so a
change of language rewrites it:

```svelte
let { closeLabel }: Props = $props();

const closeText = $derived(closeLabel ?? t('primitives.close'));
```

## Adding a language

Take the English catalogue, every `packages/svelte/locales/en*.json` merged,
which `catalogues()` in `scripts/emit-catalogues.mjs` answers. Write it to one
`<tag>.json`, translate the lines you can, and leave out the rest. Add the tag
to `LOCALES` in
`src/i18n/locale.svelte.ts`, run `pnpm build`, and the language is in the
switch. A module's catalogue is the same file under the module's own `locales/`
folder. The application's own texts, the `app` namespace with the menu entries,
are in `frontend/src/locales/`. A `<tag>.json` put there is read with no code
change.
