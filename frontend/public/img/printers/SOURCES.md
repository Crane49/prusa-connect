# Where the printer pictures come from

Drawings of Prusa machines, and of the two Trilab DeltiQ printers, as SVG
files and, where no vector source exists, PNG renders. The application serves
them from `/img/printers/`, and `PrinterIcon` in
`@prusa/connect-ui/primitives` loads one by file name. Nothing bundles
them: they are static files the browser fetches.

| file                           | the machine it draws                            |
| ------------------------------ | ----------------------------------------------- |
| `core_one.svg`                 | Original Prusa CORE One                         |
| `core_one_mmu3.png`            | Original Prusa CORE One with MMU3               |
| `core_one_oak.png`             | Original Prusa CORE One Signature Oak           |
| `core_one_indx.png`            | Original Prusa CORE One INDX                    |
| `core_one_l.svg`               | Original Prusa CORE One L                       |
| `core_one_l_mmu3.png`          | Original Prusa CORE One L with MMU3             |
| `core_one_l_indx.png`          | Original Prusa CORE One L INDX                  |
| `mini.svg`                     | Original Prusa MINI+                            |
| `mk25.svg`                     | Original Prusa MK2.5                            |
| `mk25s.svg`                    | Original Prusa MK2.5S                           |
| `mk3.svg`                      | Original Prusa MK3                              |
| `mk3mmu2.svg`                  | Original Prusa MK3 with MMU2                    |
| `mk3s.svg`                     | Original Prusa MK3S+                            |
| `mk3smmu2s.svg`                | Original Prusa MK3S+ with MMU2S                 |
| `mk3smmu3.svg`                 | Original Prusa MK3S+ with MMU3                  |
| `mk35.svg`                     | Original Prusa MK3.5                            |
| `mk35mmu3.svg`                 | Original Prusa MK3.5 with MMU3                  |
| `mk35s.svg`                    | Original Prusa MK3.5S                           |
| `mk35smmu3.svg`                | Original Prusa MK3.5S with MMU3                 |
| `mk39.svg`                     | Original Prusa MK3.9                            |
| `mk39mmu3.svg`                 | Original Prusa MK3.9 with MMU3                  |
| `mk39s.svg`                    | Original Prusa MK3.9S                           |
| `mk39smmu3.svg`                | Original Prusa MK3.9S with MMU3                 |
| `mk4.svg`                      | Original Prusa MK4                              |
| `mk4mmu2s.svg`                 | Original Prusa MK4 with MMU2S                   |
| `mk4mmu3.svg`                  | Original Prusa MK4 with MMU3                    |
| `mk4s.svg`                     | Original Prusa MK4S                             |
| `mk4smmu2s.svg`                | Original Prusa MK4S with MMU2S                  |
| `mk4smmu3.svg`                 | Original Prusa MK4S with MMU3                   |
| `xl.svg`                       | Original Prusa XL                               |
| `xlmultitool2.svg`             | Original Prusa XL, 2 tool heads                 |
| `xlmultitool3.svg`             | Original Prusa XL, 3 tool heads                 |
| `xlmultitool4.svg`             | Original Prusa XL, 4 tool heads                 |
| `xlmultitool5.svg`             | Original Prusa XL, 5 tool heads                 |
| `xl_enclosure.svg`             | Original Prusa XL with enclosure                |
| `xl_multitool_2_enclosure.svg` | Original Prusa XL, 2 tool heads, with enclosure |
| `xl_multitool_3_enclosure.svg` | Original Prusa XL, 3 tool heads, with enclosure |
| `xl_multitool_4_enclosure.svg` | Original Prusa XL, 4 tool heads, with enclosure |
| `xl_multitool_5_enclosure.svg` | Original Prusa XL, 5 tool heads, with enclosure |
| `ht90.svg`                     | Prusa Pro HT90                                  |
| `trilab_deltiq_2.svg`          | Trilab DeltiQ 2                                 |
| `trilab_deltiq_2_plus.svg`     | Trilab DeltiQ 2 Plus                            |
| `unknown.svg`                  | a generic printer, for a model with no drawing  |

## Origin

Prusa Research drew them for Prusa Connect. The first thirteen reached this
repository through pull request 185. The rest are copied byte for byte from
`connect-ui/svg/printers/` in the Prusa Connect web frontend; only
`core_one_indx.png` is renamed, from `core_one_plus_indx.png`, so that the
`CoreOne_INDX` firmware profile resolves to it.

## Licence

Prusa Research owns the artwork. The owner ruled that the first thirteen files
ship in this repository, which is question 3 of
earlier research.
They are not covered by the licence in `LICENSE` at the root, and a fork that
redistributes them needs Prusa Research's permission. No such ruling covers the
files copied from Prusa Connect yet.

## Adding a machine

A new model needs a file here named after its key, the key in `PRINTER_MODELS`
and in `RASTER_MODELS` when the file is a PNG (both in
`packages/svelte/src/primitives/printer-icon/types.ts`), and its two names in
`names.ts` beside them. `artwork.test.ts` fails on a key with no file.

A printer names its model in the handshake, and `printerModelOf` folds that
spelling to letters and digits before it looks for a key, so `CoreOneL_INDX`
finds `core_one_l_indx`. A key whose folded form matches no spelling a printer
sends is never drawn.
