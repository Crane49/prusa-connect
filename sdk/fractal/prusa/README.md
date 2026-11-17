# The Prusa interfaces as definition files

`domain-common.md` and `domain-3d-print.md` of the Fractal specification
define the interfaces of a Prusa printer. The JSON files in this folder
describe the same interfaces in the format that `sdk/fractal/schema` loads.
The repository has one copy of them. Go code reads them through the embedded
filesystem `prusa.Files`. The tests of `sdk/fractal/schema` load them and
compare them with the specification. The Fractal module loads them to build
the interfaces it serves.

## The version of the specification

The files follow the specification as the Fractal team published it on
2026-10-20.

`TestPrusaFilesMatchTheSpecification` in `sdk/fractal/schema` reads both
documents from the folder that `FRACTAL_SPECS_DIR` names on a developer
machine. It fails when an exposure, a property, method, parameter, type,
access mode, nullable flag or enumeration value differs. The test skips when
the variable is not set, so CI does not need the specification.

## The enumerations are in one file

`enums.json` defines every enumeration. `FDMPrinterHead1` and `FilamentSlot1`
both use `FDMMaterial`, so a new filament material is one added line in
`enums.json`. With a copy of the enumeration in each interface file, a new
material would be a line in each file, and the copies could differ.
