---
title: "ADR-20261026-w6a: A 3MF project is read by a core reader and one dialect per slicer"
description: "How sdk/threemf reads a 3MF project into setups, beds, objects and instances, how it chooses the reader for the slicer that wrote the file, and how it rewrites a PrusaSlicer project for a reprint."
date: 2026-10-26
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - sdk
---
# ADR-20261026-w6a: A 3MF project is read by a core reader and one dialect per slicer

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amended by: [ADR-20261028-jae](20261028-jae-3mf-projects-are-read-by-the-threemf-extension-and-the-core.md), which moves the package to `extensions/threemf/threemf` | Related: [ADR-20261021-ktp](20261021-ktp-a-print-says-which-objects-it-skipped.md)

A module that reprints part of a project, or shows a person which objects are
on which bed, has to read the project file. A 3MF project is a zip. Its core,
`3D/3dmodel.model`, is the same for every program: meshes, objects built from
them, and a build with one item for each copy of an object on the plate. Each
slicer adds its own files beside the core, and they differ. PrusaSlicer 2.x
stores no beds and finds them from where each copy stands. PrusaSlicer 3.0
stores several printer setups, each with beds of its own size. OrcaSlicer
stores plates, with the instances on each. So the package
`sdk/threemf` reads the core one way for every file, and reads what a slicer
adds through a dialect chosen for that slicer.

The package is in `sdk/` because a module builds against the SDK alone. It uses
the standard library only.

## rules

### the model

1. `threemf.Read` answers one `Project`: printer setups, each with its beds,
   and objects, each with its instances. An instance is one build item. It
   carries its transform, its printable flag, its bed, its placement on that
   bed, its box, and its outline seen from above in project coordinates.
2. A bed has an offset. A point at p on the bed is at p plus the offset in the
   project. A slicer slices a bed as if it stood at the origin, so a sliced
   file's coordinates plus the offset are project coordinates.
3. The settings a slicer stored are kept as raw text, one table per kind of
   profile, beside the few fields the package reads out of them. A reader that
   needs another setting reads the table and does not wait for a field.

### choosing the dialect

4. The dialect is chosen from the `Application` metadata of
   `3D/3dmodel.model`, read from the top of the file before the model is
   parsed. PrusaSlicer below 3.0 is `prusaslicer2`, PrusaSlicer 3.0 and later is
   `prusaslicer3`, and an `Application` of `BambuStudio` or `OrcaSlicer` is
   `orcaslicer`. A file without that metadata is chosen by the slicer files it
   carries. A program that writes its metadata after the build, as Cura does,
   gets its `Producer` from the full parse. The version is split off the name
   after a dash, or after a space or an underscore when it is dotted, so
   `Ultimaker Cura 5.2.1` is `Ultimaker Cura` at `5.2.1`.
5. A file no dialect matches is read by the generic dialect: one setup, one
   bed, every build item on it, placement `unknown`. Nothing in the file is
   guessed.
6. A new slicer is a new type that implements the unexported `dialect`
   interface in `sdk/threemf/dialect.go`. A dialect reads its own files before
   the model, groups the build items into objects, and places the instances
   on its beds. A slicer that stores plates in the file places each instance on
   the plate the file names instead of computing it.

### the PrusaSlicer dialects

7. The sources are the public PrusaSlicer repository and the public released
   command lines. The bed code of 2.x is the same in every release from
   2.9.0 to 2.9.6: `src/libslic3r/MultipleBeds.cpp`, `BuildVolume.cpp` and
   `ModelObject::update_instances_print_volume_state` in `Model.cpp`. The bed
   code of 3.0 is the same in 3.0.0-alpha11 and 3.0.0-alpha12:
   `BedTracking.cpp` and `Algorithms/Bed.cpp`. No other source is used.
8. `prusaslicer2` lays the beds out on a grid in square shells. Beds 0 to 8
   stand in the cells (0,0), (1,0), (0,1), (1,1), (2,0), (2,1), (0,2), (1,2)
   and (2,2). The gap is
   `min(100, 0.3 * the diagonal of the bed shape's box)` in both directions,
   so bed b stands at `cell * (size + gap)`. There are at most 9 beds. An
   instance past the ninth bed is on no bed.
9. `prusaslicer2` judges a model part against one bed by its vertices, as
   `BuildVolume::object_state` does. Only vertices above the plane 0.0001 mm
   under the bed count, together with the points where an edge crosses that
   plane. A rectangle is tested by its box, a round bed by the circle through
   its corners, and any other shape by its convex hull. Each is grown by
   0.0001 mm and topped at the highest print. Some points on the bed and some
   off it is colliding, all on it is inside, none on it is outside. A part with
   no vertex above the plane is below, and below counts as on the first bed.
10. Each model part of an instance takes the first bed, in index order, that
    it is not outside of. The instance takes the bed of its first model part
    that found one. It is inside when every part is inside its own bed, partly
    outside when one part is colliding or parts disagree, and outside
    otherwise. A part wholly below the bed counts for neither. The beds of a
    project are those up to the highest bed used, an empty bed between them
    included.
11. Before it judges an object, `prusaslicer2` moves it up or down as
    `ModelObject::ensure_on_bed(true)` does when PrusaSlicer opens a project.
    An object of one model part is put on the bed, unless it sinks into it. An
    object of several parts moves up only when all of it is below 0.05 mm. The
    move is used for the test only. Bounds and outlines keep the file's
    height.
12. Before 2.9.0 the beds stood in one row, a fifth of the bed's width apart.
    `prusaslicer2` tries the row first only when the Application is
    `PrusaSlicer-<version>` with a version below 2.9.0-alpha1. It keeps the
    row when every instance finds a bed on it and more than one bed is used.
    Each instance is then judged on the grid as if moved from its bed of the
    row to the bed of the same index on the grid. The beds keep the offsets of
    the row. This is `MultipleBeds::rearrange_after_load`. Any other
    file is read on the grid as it stands.
13. `prusaslicer3` reads the setups and the bed offsets from
    `Metadata/PrusaSlicer3_project.json`. It judges an instance by the convex
    hull of its model parts above the bed's plane and by their box above
    -0.001 mm. It looks through the beds setup by setup and bed by bed, as
    `BedTracking::find_bed_instance_for_bounds` does.
14. Against one bed, as `Algorithms::Bed::contains_3d` does: a box that does
    not touch the bed's box is outside. A box whose four corners are on the
    bed is inside. Otherwise the hull decides: corners on and off the bed is
    colliding, all on is inside, and none on is colliding when the hull still
    overlaps the bed. A bed that is not convex is compared with the hull as a
    polygon. There is no tolerance: a point on the edge is on the bed. An
    instance inside on the plane is outside when it starts above the highest
    print, colliding when it reaches above it, and below when it ends under
    the plane.
15. The first bed an instance is inside of or colliding with is its bed. One
    inside is sliced with that bed. One colliding keeps that bed as partly
    outside, and no later bed is tried. An instance below or outside every bed
    is on none.

### the OrcaSlicer dialect

16. OrcaSlicer writes `BambuStudio-<version>` as its `Application`, the same
    form BambuStudio writes. The two are told apart by
    `Metadata/project_settings.config`: a file with at least two of
    `elefant_foot_compensation_layers`, `gcode_label_objects`,
    `staggered_inner_seams` and `wipe_on_loops` is from OrcaSlicer, and its
    producer name becomes `OrcaSlicer`. Every OrcaSlicer project on Printables
    the rule was tried on, from 1.7 to 2.2, has all four, and no BambuStudio
    project from 1.1 to 2.3 has any. An early OrcaSlicer writes a BambuStudio
    version number, so the version does not tell them apart.
17. A BambuStudio project has the same layout and is read by the same dialect,
    with the issue `unsupported_producer`. BambuStudio is not a target: no
    rule of this dialect is written for it.
18. The sources are the public OrcaSlicer repository at the tags v1.9.0,
    v2.1.0, v2.2.0, v2.3.0 and v2.3.2, where `PartPlate.cpp` lays plates out
    the same way. Every plate of `Metadata/model_settings.config` is a bed of
    the one setup, in plate order, with the plate's name and locked flag. The
    plates stand `ceil(sqrt(plates))` to a row, rows going towards -Y. The
    stride is 1.2 times the width and the depth of `printable_area`, each cut
    to whole millimetres. The height is `printable_height`.
19. OrcaSlicer does not use the instances `model_settings.config` lists for
    a plate. When it opens a project, `PartPlateList::reload_all_objects` puts
    each instance on the first plate whose box its box overlaps by more than
    a touch. The plate's box is its shape's box grown by 0.0001 mm, from the
    plate up to the printable height. The dialect does the same. Its
    placement on that plate compares the outline with the plate's convex hull,
    and no OrcaSlicer source was checked for it.
20. An object's name, and the name and type of each of its parts, come from
    `Metadata/model_settings.config`. Each component of an object is one part,
    keyed by the id of the object it points at, in whichever model file that
    object is. `Metadata/plate_<n>.png` shows plate n.
21. A printer that feeds several filaments into one hot end, with
    `single_extruder_multi_material` set, has one nozzle. PrusaSlicer and
    OrcaSlicer store a `nozzle_diameter` for every extruder, and such a
    printer has one extruder per filament.

### reading the 3MF core

22. Part names follow the Open Packaging Conventions. A name in a
    relationship or a `p:path` is a URI, so `%20` stands for a space, and names
    compare without regard to case. The exact name wins.
23. The `unit` of a model scales every vertex and the move of every
    transform, as the core specification says. PrusaSlicer 2.9.6 scales the
    vertices only, so a file not in millimetres with a move in its transforms
    stands elsewhere in PrusaSlicer. PrusaSlicer writes millimetres.
24. A build item without `printable` is printable. No 3MF specification has
    the attribute. PrusaSlicer writes it and reads a missing one this way.
25. An extension the model lists in `requiredextensions` that this package
    does not read gives the issue `unsupported_extension` with its
    namespace. The production and materials extensions are read, or do not
    change a shape. The read goes on, as in PrusaSlicer, which does not check
    the list.
26. A `p:path` on a component of a model file that is not the root is
    `ErrFormat`. The production extension says a consumer must report an
    error for such a package.

### reading safely

27. `Read` refuses an archive that declares more entries than the limit,
    before `archive/zip` builds a record for each, and an archive with two
    entries of one name.
28. Every read is held to `Limits`: entry sizes, bytes of model XML, settings
    files, pictures, objects, build items, component depth, and the vertices
    kept in memory. A mesh past the vertex limit is drawn from its box, the
    instance says so in `OutlineApprox`, and the read goes on.
29. A path written inside the archive, in a relationship or a `p:path`, is
    resolved inside the archive and refused when it climbs out of it. No name
    from the archive reaches a file system.

### rewriting for a reprint

30. `threemf.Rewrite` copies a PrusaSlicer project for a `Selection`: one bed
    and the build items to print on it. The selected items stay printable, every
    other item becomes non-printable, and the chosen bed becomes the one a
    slicer's command line slices. A project of any other dialect is
    `ErrUnsupported`. Making another program's file slice the same way in
    PrusaSlicer is not attempted. PrusaSlicer reads such a file by rules of
    its own, such as its own beds for OrcaSlicer plates.
31. For `prusaslicer2`, the printable attribute of each build item is set, the
    chosen bed's items move onto bed 0, and bed 0's items move to where the
    chosen bed was. The bed indices of the wipe tower and of the colour
    changes swap with them. PrusaSlicer 2.9.6 `--export-gcode` slices bed 0
    only.
32. For `prusaslicer3`, the project file lists every unselected item as not
    printable, and keeps only the chosen setup with only the chosen bed. The
    model is not changed. PrusaSlicer 3.0.0-alpha12 `--export-gcode` slices
    the first bed of the first setup.
33. Every entry the rewrite does not change is copied with its compressed
    bytes as they are.

### mapping a sliced file back

34. `ParseObjectsInfo` reads the `objects_info` value a slicer writes into
    G-code. `Project.MatchLabels` pairs each labelled object with the
    instance it came from by the centres of their outlines, after moving the
    label by the bed's offset. A name is used only to break a tie, because
    the slicer numbers instances per bed and two objects may share a name.
    It is made for a file PrusaSlicer sliced from a PrusaSlicer project, the
    pair `Rewrite` produces. For any other pair it promises nothing.

### extruders, painting and layer changes

35. Each setup lists its `Filaments`, one per extruder. A printer with an MMU
    has one per slot. The colour is the one the slicer shows the extruder in.
    PrusaSlicer takes `extruder_colour` and, where it is empty, the
    `filament_colour` of the same extruder:
    `Plater::get_extruder_color_strings_from_plater_config` of 2.9.6.
    PrusaSlicer 3.0 keeps `extruder_colour` in its project settings and
    `filament_colour` in its filament settings. OrcaSlicer shows the
    `filament_colour`. An empty value in such a list holds its place.
36. The extruders of a part are resolved per role. A role is one kind of
    extrusion with a setting of its own. PrusaSlicer has
    `perimeter_extruder`, `infill_extruder` and `solid_infill_extruder`.
    OrcaSlicer has the `*_filament_id` settings, or the older
    `wall_filament`, `sparse_infill_filament` and `solid_infill_filament`. The
    print's settings come first, then the object's, then a layer range, then
    the part's. In PrusaSlicer the `extruder` setting of a level sets every
    role, and a role that is not 0 then wins:
    `apply_to_print_region_config` in `src/libslic3r/PrintObject.cpp` of
    2.9.6. In OrcaSlicer a role of 0 takes the `extruder` setting. An
    extruder past the printer's last is the first one:
    `clamp_exturder_to_default`, and in OrcaSlicer the load of the project.
    `Part.Extruder` is the outer wall. `Object.Extruders` holds every role.
37. A model part always has extruders. A modifier has them only when it sets
    an extruder itself, laid over its object's. A negative volume, a support
    blocker and a support enforcer print nothing. OrcaSlicer drops the
    `extruder` of the part of an object with one part, and the object's
    applies. Only model parts count for the bounds, the outline and the bed
    of an instance, as `ModelObject::instance_bounding_box` of OrcaSlicer
    and the bed rules above do.
38. Painted triangles are read from `slic3rpe:mmu_segmentation`,
    `slic3rpe:custom_supports` and `slic3rpe:custom_seam` in PrusaSlicer 2.x,
    from `Metadata/Slic3r_facets_annotation.json` in PrusaSlicer 3.0, and from
    `paint_color`, `paint_supports` and `paint_seam` in OrcaSlicer. A
    triangle's text is a bitstream, one hexadecimal digit a nibble, with the
    first nibble last. Only the states of its leaves are read. Up to state
    16 the slicers agree. Above it PrusaSlicer 2.9.6 reads `0b1110` and two
    more nibbles (`TriangleSelector::deserialize`), and OrcaSlicer reads
    `0b1111` and one more.
39. Painting with extruders is used only on a printer with more than one
    extruder. A state past the last extruder is ignored. The sources are
    `is_mm_painted` in `PrintApply.cpp` and `num_facets_states` in
    `MultiMaterialSegmentation.cpp` of 2.9.6. State 0 is the part's own
    extruder. A part painted on every triangle does not use its own.
40. An object prints support when its settings turn support on, when it
    enforces support on its lowest layers, or when it has a raft:
    `PrintObject::has_support_material`. Support may be set to go only where
    a person asked for it. That is `support_material_auto` 0 in PrusaSlicer
    2.x, `enforcers_only` in 3.0 and a `(manual)` `support_type` in
    OrcaSlicer. Then the object needs an enforcer or an area painted to have
    support. The support
    and interface extruders then count, unless they are 0.
41. Layer ranges are read from `Metadata/Prusa_Slicer_layer_config_ranges.xml`
    in PrusaSlicer 2.x and `Metadata/layer_config_ranges.xml` in OrcaSlicer,
    by the object's position counted from 1, and from each object's `ranges`
    in PrusaSlicer 3.0. A range that sets an extruder prints the model parts
    in it with that extruder, unless the part sets its own.
42. Layer changes are kept per bed. PrusaSlicer 2.9 writes one
    `custom_gcodes_per_print_z` element per bed, side by side at the top of
    the file, each with a `bed_idx`. PrusaSlicer 3.0 keeps a `custom_gcode`
    in each bed. OrcaSlicer names the plate of each group of changes with a
    `plate_info` in `Metadata/custom_gcode_per_layer.xml`. A file with no bed or
    plate holds bed 0 or plate 1. The types 0 to 4 are a colour change, a
    pause, a tool change, a template and custom G-code. A change written
    before PrusaSlicer 2.3 has no type and is read by its G-code. A change for
    a bed that does not exist is dropped.
43. `Project.Extruders` joins the extruders of every object and those a tool
    change switches to.
44. A bed is `Sequential` when `complete_objects` is set in PrusaSlicer, or
    when the plate's `print_sequence`, or else the project's, is `by object`
    in OrcaSlicer: `PartPlate::get_real_print_seq`.
45. An OrcaSlicer plate names its G-code in `gcode_file`. The bed keeps the
    name in `GCode` when the archive holds that entry. A project with no
    object and G-code for a bed, as a `.gcode.3mf` file is, has the issue
    `gcode_only`.
46. No slicer read here has object groups. What a person sees as a group is
    one object with several parts, and it is one object here. PrusaSlicer
    2.x writes each further instance as a build item of its own, and the
    item is the instance. The `assemble` list of OrcaSlicer places objects
    for its assembly view and does not change a plate. Its filament groups
    map filaments to the nozzles of a printer with several, and its colour
    groups give extruders to a plain 3MF file. Neither is read.

### known limits

47. A zip entry whose name is not UTF-8 is not found by the UTF-8 path that
    points at it. A model part a Chinese Windows build named in GBK is one
    such entry. The component is reported as `unknown_reference` and left out.
48. The model XML is parsed with `encoding/xml`, at about 35 MB a second. A
    project with a quarter of a gigabyte of model XML takes several seconds.
49. The rules judge a mesh by the vertices and triangles kept within
    `MaxVertices`. A mesh past it is judged by its box. Past sixteen times
    that many vertex visits in one read, a part is judged by its outline and
    box. On the Printables corpus no file reaches either limit.
50. A project may be laid out on the row with an instance that sticks out of
    its bed far enough to reach another bed of the grid. That instance may get
    a bed whose offset of the row does not match where it stands.
51. A command line does not judge a project as the slicer does when it
    opens it. Both command lines put every object on the bed before they
    slice, unless `--no-ensure-on-bed` is given, so an object that sinks into
    the bed prints whole. The 3.0.0-alpha12 command line prints an instance
    that sticks out of a bed whose shape does not start at 0,0, moved by that
    corner. PrusaSlicer 2.9.0 slices every printable instance, on any bed.
52. The full-spectrum extruders of PrusaSlicer 2.9.6, in
    `Metadata/Prusa_Slicer_full_spectrum.json`, are not read. An extruder
    number past the physical extruders becomes the first one, as in a
    slicer without them. The wipe tower extruder is not counted.

## enforcement

Each rule is pinned by a test in `sdk/threemf`.

| rule | test |
|---|---|
| 8, grid, gap, nine beds | `TestPS2GridOffsets` for MK4, XL and MINI beds, `TestPS2OffsetRoundTrip` on random cubes |
| 9, vertices, tolerance, bed shapes | `TestGoldenBedsAgainstRecordedSlicers` (`ps2_rect`, `ps2_circle`, `ps2_neg`, `ps2_l`), `TestPS2VertexRule` |
| 10, first part with a bed, below | golden probes `r_first_on_bed0`, `r_first_on_bed1`, `r_below_first`, `r_modifier_first` |
| 10, first part with a bed, bed count | `TestPS2FirstPartWithABed`, `TestPS2BedCount` |
| 11, height on opening | `TestPS2HeightOnOpen` |
| 12, row of the releases before 2.9 | `TestReadPrusaSlicer281LegacyRow`, `TestReadPrusaSlicer2ParkedInstanceKeepsTheGrid`, `TestPS2LegacyRowNeedsPrusaSlicer` |
| 13 to 15, PrusaSlicer 3.0 beds | golden `ps3_rect`, `ps3_offset`, `ps3_circle`, `ps3_l`, `ps3_order`, `ps3_below`, `TestReadPrusaSlicer3TwoSetups` |
| 18 and 19, OrcaSlicer plates | `TestReadOrcaSlicerPlates`, `TestOrcaPlatesFromGeometry` |
| 21, one hot end | `TestPS2NozzlesPerHotEnd` |
| 22 to 24, the 3MF core | `TestSpecUnitInch`, `TestSpecPartNames`, `TestSpecPrintableDefault`, `TestReadGenericFile` |
| 25 and 26, extensions | `TestSpecRequiredExtensions`, `TestSpecPathOnlyInRootModel` |
| 27 to 29, reading safely | the hostile tests, `FuzzRead` |
| 30 to 33, rewriting | the rewrite tests, `TestRewriteRefusesOrcaSlicer`, `TestRewriteRefusesOtherProducers` |
| 35 to 39, PrusaSlicer 2.x extruders and painting | `TestPS2Multimaterial`, `TestPS2OneExtruderIgnoresPainting`, `TestPS2LongPaintState`, `TestPS2RolesAndLayerRanges` |
| 40, support | `TestPS2SupportExtruders`, `TestOrcaMultimaterial` |
| 36 to 45, OrcaSlicer | `TestOrcaMultimaterial`, `TestOrcaLayerChangesWithoutPlates`, `TestReadGCodeOnly` |
| 35 to 44, PrusaSlicer 3.0 | `TestPS3Multimaterial` |
| 42, layer changes before 2.3 | `TestPS2LegacyLayerChanges` |

- The golden fixtures in `sdk/threemf/testdata/golden` are synthetic
  projects. `gen.py` writes them from the probes in `cases.py`, and
  `recorded.json` holds what the public PrusaSlicer 2.9.6 and 3.0.0-alpha12
  command lines printed for each, with `--no-ensure-on-bed`. Each probe tests
  one rule at an edge, such as a box 0.00005 mm past the bed.
- `TestGoldenBedsWithLiveSlicers` and `TestRewriteSlicesWithPrusaSlicer` run
  the real command lines when `THREEMF_PRUSASLICER2` or
  `THREEMF_PRUSASLICER3` names a binary. The first fails when a slicer's
  answers no longer match `recorded.json`.
- PrusaSlicer 3.0.0-alpha11 prints the same objects as alpha12 for every
  golden fixture.
- The extruders of rules 36 to 40 were checked against the command lines on
  30 public multi-material projects for MMU, XL and CORE One INDX printers.
  For each, the extruders of the instances on bed 1 matched the extruders
  with filament used in the G-code PrusaSlicer 2.9.6 wrote, 30 of 30. The
  same projects converted with the 3.0.0-alpha12 command line matched 24 of
  24 it could slice. It does not finish the G-code of three INDX projects.
- `make sdk-check` refuses any import outside the standard library.

## rejected

| option | why not |
|---|---|
| one reader with checks for each slicer inside it | each new slicer would touch every step. A dialect keeps a slicer's rules in one file |
| rewriting OrcaSlicer or plain 3MF files for PrusaSlicer | PrusaSlicer lays out such a file by its own rules. The copy would promise a print it cannot keep |
| choosing between the row and the grid by which places more instances | 2.9.6 does not, so the bed numbers would differ from the ones it shows and the ones `Rewrite` swaps |
| telling OrcaSlicer from BambuStudio by version number | an early OrcaSlicer writes BambuStudio's version numbers |
| a dialect interface that third parties implement | there is one caller. An exported interface is a contract with a conformance suite |
| the bed of an instance from its origin | the origin is not always the centre of the object, and PrusaSlicer itself judges by the mesh |
| the bed of a PrusaSlicer 2.x instance from where its outline overlaps a bed | PrusaSlicer 2.9 tests vertices. A bar laid across a bed with its ends off it is on no bed there |
| the plates `model_settings.config` lists for each instance | OrcaSlicer places instances by where they stand when it opens a project |
| a 3D convex hull of each mesh | the slicers judge vertices, or the hull above the bed's plane |
| decoding the whole project file into maps and writing it back | it reorders members the rewrite does not change |
| re-encoding every zip entry | it would change bytes a slicer reads, such as painted triangles, for no reason |
