# Where these files came from

Inter 4.1, the release rsms/inter publishes on GitHub. No font package from a
registry: a face is a binary this repository serves itself, and the record of
what it is belongs beside it (`security-supply-chain.md`).

| file                  | sha256                                                             | bytes  |
| --------------------- | ------------------------------------------------------------------ | ------ |
| `InterVariable.woff2` | `693b77d4f32ee9b8bfc995589b5fad5e99adf2832738661f5402f9978429a8e3` | 352240 |
| `OFL.txt`             | `262481e844521b326f5ecd053e59b98c8b2da78c8ee1bdbb6e8174305e54935a` | 4380   |

Both come out of one archive:

| what                  | value                                                                |
| --------------------- | -------------------------------------------------------------------- |
| release               | <https://github.com/rsms/inter/releases/tag/v4.1>                    |
| archive               | <https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip> |
| sha256                | `9883fdd4a49d4fb66bd8177ba6625ef9a64aa45899767dde3d36aa425756b11e`   |
| `InterVariable.woff2` | `web/InterVariable.woff2` in the archive                             |
| `OFL.txt`             | `LICENSE.txt` in the archive                                         |

## What the file holds

One variable face, `wght` 100 to 900 and `opsz` 14 to 32, so every weight the
interface asks for is one request. It maps 2852 characters: Latin, Latin
Extended (`ř ž ě ů ď ł ő`), Greek, Cyrillic, the currency signs and the arrows.
Its `tnum` feature is what `tabular-nums` turns on for a reading.

The release ships no per-script subset, and cutting one here would mean
re-encoding the binary and losing the checksum above. So the whole face ships,
at 344 KiB, and the page preloads it.

## The licence

The SIL Open Font License, version 1.1, in `OFL.txt`. It allows this
repository to carry the file and allows the built application to serve it. The
licence must reach whoever receives the font, so `OFL.txt` sits beside it and
is built into the application the same way.

## Replacing it

Download the archive for the new version, check its sha256, take the same two
files out of it, and rewrite the three sums above in the same commit.
