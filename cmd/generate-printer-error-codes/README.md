# Prusa printer error codes

`make generate-printer-error-codes` downloads `yaml/buddy-error-codes.yaml` and
`yaml/mmu-error-codes.yaml` from one commit of
[Prusa-Error-Codes](https://github.com/prusa3d/Prusa-Error-Codes) and rewrites
`packages/svelte/locales/en.printerCode.json`, the English catalogue of the
interface. The commit is the `_commit` line of that file. No copy of the two
files is kept here.

The printer's dialog on the screen reads the catalogue for a dialog that came
with a code and no words. A translator writes the same keys in
`packages/svelte/locales/<language>.json`, and a key a language leaves out is
shown in English. The server sends the code on and holds no text for it.

Most codes say the same on every printer, so the words are written once, under
the code as the catalogue writes it: `printerCode.xbuddy.XX807` for `13807`. Where one
code has several texts, the one for the most printers keeps `XX`. Each of the
others is written once too, with its printers in the key:
`printerCode.xbuddy.XX301.p17_41` is what the XL and the XLP say. The dialog
reads those keys first and the `XX` one after them.

Prusa Research publishes Prusa-Error-Codes under the GPL v3 and owns it. It
allows Prusa Connect to ship these texts under its own licence.

## Taking a newer catalogue

Nothing runs it on a schedule. A person runs it after a firmware release. A
code the catalogue does not hold yet shows as its number.

```sh
make generate-printer-error-codes COMMIT=latest
make generate-printer-error-codes COMMIT=<full commit hash>
```

`latest` is the newest commit of the `master` branch of Prusa-Error-Codes.

With no `COMMIT` the target writes the file again from the commit it records,
so `git diff` then shows a manual edit of the file. It needs the network, so no
gate runs it.

The firmware fills some texts in as it shows them, such as `Dock %d`. Those
codes get their title and no text, and the dialog sends the reader to the
printer for the rest.
