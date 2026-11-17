---
title: "ADR-20261027-4tq: A 3MF project in a storage is read whole and answered as one document"
description: "How the threemf extension answers what a 3MF project file in a storage holds: one route for the project, one for its pictures and one for an object as triangles, in bed coordinates, with a settings summary and the raw settings only on request, and how the page of the file lays the project out."
date: 2026-10-27
status: accepted
supersedes: []
amends: []
tags:
  - adr
  - api
  - modules
---
# ADR-20261027-4tq: A 3MF project in a storage is read whole and answered as one document

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Related: [ADR-20261026-w6a](20261026-w6a-a-3mf-project-is-read-by-a-core-reader-and-one-dialect-per.md)

A page about a 3MF file in a storage shows the project: which printers it was
laid out for, the beds, what stands on each bed, and the settings. The reader
for this is the Go package of the threemf extension (ADR-20261026-w6a), and
the routes and the page are that extension's too, at
`/api/v1/modules/com.github.aux.prusa3d.threemf`. The core's stat route
answers a print file's details from a bounded read of its head, and it is
called for every row a folder shows. A project is a zip whose model must be
read whole to place one instance, so it gets routes of its own.

## rules

### the routes

1. `get-project` (`GET <mount>/project?storage=&path=`)
   answers one `Project` document: the producer, counts, the setups with
   their beds, the objects with their instances, the pictures and the issues.
   A page asks once and draws everything from it.
2. `get-project-thumbnail` (`GET <mount>/project/thumbnail?storage=&path=&index=`)
   serves one picture by its index in `thumbnails`, with an entity tag of the
   file's version and the index. A bed names its picture by that index. Pictures are
   never inlined into the document.
3. The host takes `view_resource` on the extension's instance. The extension
   then reads the file through the storage wire as the person the host
   named. So the storage takes `read_storage` for that person and refuses a
   climbing path, as every other read of a storage's contents does.

### the document

4. An instance's `outline` and `min` are in the coordinates of its bed. A page
   that draws one bed draws it at the origin. A page that draws the whole
   project adds the bed's `offset`, which reproduces the slicer's layout of the
   beds. An instance on no bed is in project coordinates, which every offset is
   measured in. An outline is sent with at most 64 points of the hull, rounded
   to a hundredth of a millimetre. A project of a thousand copies then stays a
   document a phone can read.
5. Each setup carries `settings`, a summary the extension reads out of the
   slicer's tables: layer heights, infill, perimeters, supports, brim, wipe
   tower and the filaments with their colours. The words are the API's own and
   do not depend on the slicer.
6. The raw tables are sent only for `raw_settings=true`. A project holds
   hundreds of settings per setup, and a page shows a dozen.
7. `producer.supported` is false for a file read by a dialect made for another
   program. `producer.dialect` says which reader read it, and `generic` is a
   file read from the 3MF core alone.

8. The document says what the project prints with, as the reader reads it
   (ADR-20261026-w6a rules 35 to 46). The project, each object and each part
   name their extruders, counted from 1. A part names the extruder of its outer
   wall and the extruders its painting adds, and an object names every
   extruder it uses. Each bed names its layer changes, whether it prints
   object by object, and the entry of any G-code the slicer stored. The
   filaments of the settings summary take the reader's colours, which follow
   the colour each slicer shows. A field that holds nothing is left out, so a
   client written before these fields reads the same document.
9. A file with G-code for its beds and no object has the issue `gcode_only`.
   The page says the file prints as it is, and draws no plan and no object
   table for it.

### errors and bounds

10. A file whose name does not end in `.3mf`, and one `threemf` refuses as a
   format, answer `threemf/NOT_A_PROJECT`. A file past the `threemf`
   limits answers `threemf/PROJECT_TOO_LARGE`, naming the limit in
   `field`. Both are 422: the file is there, and asking again changes nothing.
11. The answer for one version of one file, keyed by its length and its
   modification time, is kept in memory, at most 8 projects and 64 MiB. A
   project that failed to read is kept too, except a read stopped by its
   deadline. Nothing is written to disk.
12. At most two projects are read at once. One read is shared by every caller
    asking for the same file and runs under its own deadline of two minutes.

### one object in 3D

13. `get-project-mesh` (`GET <mount>/project/mesh?storage=&path=&object=`) answers
    the model parts of one object as at most 20000 triangles. The object
    stands as its first copy stands: turned and scaled, not moved, centred on
    x 0 and y 0 with its lowest point at z 0. Modifiers, negative volumes and
    support blockers or enforcers are left out.
14. A mesh with more triangles is made coarser by merging the vertices that
    fall in one cell of a grid, and the grid grows coarser until the
    triangles fit. `source_triangles` says how many the file has. A mesh the
    reader did not keep, because it was past `MaxVertices`, is sent as the box
    around it, with `box` set.
15. Each part names the extruder of its outer wall, and a page colours the
    part with that extruder's filament. The painting on a surface is not
    sent: a painted object is drawn in the colour of its parts.
16. An index the project has no object at answers 404 with
    `threemf/NO_OBJECT`. The errors of rule 10 apply.
17. A mesh read is a whole read of the file, through `threemf.ReadMesh`. It
    takes one of the two read slots of rule 12 and the same deadline. One
    mesh for one object of one version of one file is kept in memory, at most
    16 meshes and 32 MiB.

### the page of a project

18. The page shows the project in three parts, in this order: an overview,
    the beds and their objects, and tabs with the settings and the detail of
    each bed. A fact a person does not check before printing is in a tab or
    in the object panel, never on the page itself.
19. The overview shows the slicer's picture of the project, its title, the
    printers, the program that saved it, the filaments the objects print with
    and one line of counts. What could not be read is one line that says how
    many things there are and opens to list them. On a file's page the core's
    header shows the picture and the overview leaves it out, so the picture
    is shown once. The view another module draws shows it.
20. The plan draws one view at a time: one bed, every bed, or the copies on
    no bed. A strip of the beds, with the slicer's picture of each, switches
    the view. A project opens on the first bed with a copy that prints.
21. A copy is drawn in the colour of the filament of its object's first model
    part. Its state does not rest on colour. A copy left out of the print is
    grey and dashed. One partly off its bed has a warning outline, and one on
    no bed a destructive one. The label and the tip of each copy say the state
    in words.
22. Beside the plan, a list names the objects of the view with a box to tick
    their copies. Only a copy that stands whole on a bed can be ticked,
    because only such a copy can be printed. A copy left out of the print can
    be ticked: printing a selection prints exactly the ticked copies. The
    pointer on a copy or a row marks the object in both places.
23. What is ticked is the view's own state, held by one `ProjectBrowser` for
    each view. Its `selections` are one entry per bed with the build items by
    `Instance.item`. That is the shape `threemf.Selection` takes, so an act
    that prints or slices the ticked copies reads them as they are.
24. An object opens in a panel at the side. The panel shows its picture, its
    size, the extruders it prints with, its painting, its parts and layer
    ranges, and each of its copies with a box to tick it. The picture is the
    3D mesh of rule 13, asked for when the panel opens. It is the view from
    above where the browser has no WebGL or the mesh could not be read. The code that
    draws in 3D is a chunk of its own, loaded when the first object opens.
25. A file of G-code alone shows the overview, a notice that it prints as it
    is, and the tabs. It shows no plan and no list.

## enforcement

The gates are the extension's tests, in `extensions/threemf`.
`main_test.go` reads the reader's fixtures through the routes and holds rules
1 to 3, 4, 6, 10, 11 and 16. `internal/document/view_test.go` holds rules 4
and 8, and `internal/projects/projects_test.go` holds rules 11 and 12.
`threemf/mesh_test.go` holds rules 13 and 14. `corpus_test.go` reads a corpus
of projects, and damaged copies of each, through every route.
`browser.svelte.test.ts` holds rules 20, 22 and 23, and
`project-plan.svelte.test.ts` holds rule 21. `project-view.svelte.test.ts`
holds rules 18, 19, 24 and 25. `openapi_test.go` holds the route shapes to
`openapi.json`, and the extension's gate generates the typed client from it.

## rejected

| option | why not |
|---|---|
| put the project into the stat route's `print` object | stat runs for every row of a folder, and a project needs a whole read |
| outlines in project coordinates only | every page that draws one bed would subtract the offset itself, and get it wrong for a bed of another setup |
| pictures as data URLs in the document | a project of twenty plates carries twenty pictures, and a page that shows two of them would download all |
| raw settings in the default answer | hundreds of keys per setup that no default view reads |
| a cache on disk | a second copy of somebody's model that nobody asked this server to keep, as for the previews |
| a mesh of every object in the project document | a project of a hundred objects would send megabytes that a page draws one object of |
| a 3D library such as three.js for the viewer | about 150 kB of gzip for one panel; the viewer here is a WebGL program of 2.4 kB of gzip |
| the painted colours in the mesh | the painting is a tree of smaller triangles per triangle, and drawing it would send many times the triangles of rule 13 |
| the whole project as one long page | a person reads a wall of evenly weighted facts to find the two they need, which is what rule 18 exists to stop |
