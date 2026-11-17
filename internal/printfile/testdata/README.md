# The print files these tests read

`cube10.stl` is a 10 mm cube written as an ASCII STL. It holds triangles and no
picture, so `Read` reports nothing for it.

The other three files are what PrusaSlicer 2.9.6 exported from that cube. The
slicer was the Windows portable build of `version_2.9.6`, from
<https://github.com/prusa3d/PrusaSlicer/releases>, with the PrusaResearch vendor
profiles it ships.

`small.ini` below is a five line overlay that keeps the files small: one
perimeter, no infill, no skirt and a 0.3 mm layer. `text.ini` holds
`binary_gcode = 0`, so the slicer writes the text form.

```sh
prusa-slicer-console.exe \
  --datadir ./datadir \
  --printer-profile "Original Prusa MK4 Input Shaper 0.4 nozzle" \
  --print-profile "0.20mm STRUCTURAL @MK4IS 0.4" \
  --material-profile "Prusament PLA @PGIS" \
  --load small.ini --load text.ini \
  --export-gcode --output cube10.gcode cube10.stl

prusa-slicer-console.exe ... --load small.ini --export-gcode --output cube10.bgcode cube10.stl
prusa-slicer-console.exe ... --load small.ini --export-3mf   --output cube10.3mf    cube10.stl
```

```ini
; small.ini
layer_height = 0.3
first_layer_height = 0.3
perimeters = 1
top_solid_layers = 1
bottom_solid_layers = 1
fill_density = 0%
skirts = 0
```

## Files for more than one tool

The four `cube10-*` files are the same cube sliced for two printers with more
than one tool, by the same PrusaSlicer 2.9.6, here the macOS build. Its
`--datadir` held the shipped `PrusaResearch.ini` under `vendor/` and a
`PrusaSlicer.ini` that installs the two printer models:

```ini
[vendor:PrusaResearch]
model:XL5IS = 0.4
model:MK4SMMU3 = 0.4
```

`multi.ini` and `mmu.ini` give the perimeters, the infill and the solid infill
to different tools, so the print uses some tools and leaves the others unused.
The MMU file has no wipe tower, because the command-line slicer stopped with a
crash while it built one.

```ini
; multi.ini, for the XL
fill_density = 15%
perimeter_extruder = 1
infill_extruder = 3
solid_infill_extruder = 2
wipe_tower = 0

; mmu.ini, for the MK4S with an MMU3
fill_density = 15%
perimeter_extruder = 1
infill_extruder = 2
solid_infill_extruder = 2
wipe_tower = 0
```

Both files also carry every line of `small.ini` except `fill_density`.

```sh
prusa-slicer --datadir ./datadir \
  --printer-profile "Original Prusa XL - 5T Input Shaper 0.4 nozzle" \
  --print-profile "0.20mm SPEED @XLIS 0.4" \
  --material-profile "Prusament PLA @XLIS;Prusament PETG @XL;Prusament ASA @XL;Prusament PLA @XLIS;Prusament PETG @XL" \
  --load multi.ini --load text.ini --export-gcode --output cube10-xl5t.gcode cube10.stl

prusa-slicer --datadir ./datadir \
  --printer-profile "Original Prusa MK4S MMU3 0.4 nozzle" \
  --print-profile "0.20mm SPEED @MK4S 0.4" \
  --material-profile "Prusament PLA @MK4S;Prusament PETG @MK4S;Prusament PLA @MK4S;Prusament PLA @MK4S;Prusament PLA @MK4S" \
  --load mmu.ini --load text.ini --export-gcode --output cube10-mk4s-mmu3.gcode cube10.stl
```

The `.bgcode` files are the same commands without `--load text.ini`.

## The corpus: one cube for every Prusa printer profile

`corpus/` holds the same cube sliced for every current Prusa printer model that
PrusaSlicer 2.9.6 has a profile for. It covers several nozzles and materials
for each model. `TestCorpusGolden` reads each file and compares the result with
a golden file in `corpus/golden/`. The printcompat tests in `sdk/printcompat`
read the same golden files and judge each file against a set of printers.

The slicer was the macOS build of `version_2.9.6`, `PrusaSlicer-2.9.6.dmg` from
<https://github.com/prusa3d/PrusaSlicer/releases>. The script used only the
`PrusaResearch.ini` vendor bundle that this build ships.

To make the files again, give the script the slicer binary. Then rewrite the
golden files and read the diff:

```sh
internal/printfile/testdata/corpus/make.sh \
  /path/to/PrusaSlicer-2.9.6/PrusaSlicer.app/Contents/MacOS/PrusaSlicer
go test ./internal/printfile/ -run TestCorpusGolden -update
(cd sdk && go test ./printcompat/ -run TestCorpusMatrix -update)
```

The script refuses a binary that is not 2.9.6. It builds a temporary data
directory with the bundle and a `PrusaSlicer.ini` that installs every printer
model of it. Then it slices each row of `corpus/corpus.tsv`.

### What the list says about each file

`corpus/corpus.tsv` has one row per file. The script reads the profiles and the
overlay from it, and `TestCorpusMatchesItsList` holds what `Read` returns to it.

| column | what it is |
|---|---|
| `file` | the name, without `.bgcode` or `.gcode` |
| `family`, `model`, `variant` | the printer, such as MK4S, and the nozzle variant, such as HF0.4 |
| `nozzle_mm`, `high_flow` | the nozzle the printer profile names |
| `materials` | the filament type of each tool, as the filament profiles name it |
| `abrasive` | the tools whose filament profile says the filament is abrasive, or `-` |
| `tools`, `tools_used` | the tool count of the profile, and the tools the print uses |
| `nozzle_checks` | how many `M862.1` lines the printer profile writes |
| `printer_profile`, `print_profile`, `filament_profiles` | the profile names given to the slicer |
| `overlay` | the file in `corpus/ini/` loaded last, or `-` |
| `text` | 1 when a text `.gcode` file is written next to the `.bgcode` file |
| `model_id` | the model the `M862.3` line names |

Every slice loads `ini/small.ini`, then a layer height, then the overlay.
`small.ini` gives one perimeter, one solid layer at the top and the bottom, no
infill and no skirt. The layer is 0.3 mm, or 0.15 mm for a 0.25 or 0.3 mm
nozzle. A bgcode slice also loads `binary.ini`, and a text slice loads
`text.ini`. Ten rows also have a text G-code file, one for each family and
two of the multi-tool files. `TestCorpusGolden` checks that the text and the
bgcode file of one slice read the same.

### What the slicer cannot do here

- A wipe tower makes the command-line slicer crash, with exit code 139, for an
  MMU3 profile and for an XL with more than one tool. The overlays for more
  than one tool set `wipe_tower = 0`.
- There is no profile for an XL with three or four tools. `xl5t-04-3of5` and
  `xl5t-04-4of5` are the 5T profile with three and four tools used, which is
  the job such a printer receives.
- The cube gives only three parts to print: the perimeters, the infill and the
  solid infill. A raft of two layers gives the fourth and the fifth tool a
  part, in `xl-4of5.ini` and `xl-5of5.ini`.
- One profile covers the MK3.5 and the MK3.5S, and one covers the CORE One and
  the CORE One+. Each writes one `M862.3` model for both printers.
- The MINI profile without input shaping writes no `M862.1` line and no
  `M862.6` line.
- `Prusament PETG Carbon Fiber @MINI` does not mark the filament as abrasive,
  so `mini-is-04-petgcf` asks for no hardened nozzle.

### Printers the corpus leaves out

The MK2, MK2.5, MK3 and MK3S profiles, with or without an MMU, are older
printers, and the corpus leaves them out. The bundle has no profile for the iX
or for a CORE One with INDX. The SL1 is a resin printer and has no G-code.

## No picture in these three

PrusaSlicer renders a thumbnail in its window and not on the command line, so
none of the three carries one, whatever `--thumbnails` is set to. The settings
these files do carry are real: the print time, the filament, the layer height,
the nozzle, the material and the printer model all come from the slicer.

The picture path is covered by the fixtures the tests build themselves, in
`fixtures_test.go`. Those are made in Go and are not slicer output, and the test
names say so.

## Errors

`Read` never reports one. A file it cannot read answers an empty `Preview`.
