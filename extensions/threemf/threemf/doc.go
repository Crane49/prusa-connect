// Package threemf reads a 3MF project file and writes a changed copy of one.
//
// A 3MF file is a zip archive. Its 3D/3dmodel.model entry holds the meshes,
// how they are put together, and the build: one item for each copy of an
// object that sits on the plate. A slicer adds files of its own beside it:
// the printer and print settings, the names of the objects, and which beds
// exist. This package reads both layers and answers one Project. A project
// has printer setups, each setup has beds, and each bed holds instances of the
// project's objects.
//
// The core of the 3MF specification is read the same way for every file.
// What a slicer adds is read by a dialect, chosen from the Application entry
// the slicer writes into 3D/3dmodel.model. This package has dialects for
// PrusaSlicer 2.x, for PrusaSlicer 3.0 and for OrcaSlicer, whose plates become
// beds. A BambuStudio project is read as an OrcaSlicer one, with an issue
// saying its producer is not supported. For a file from any other program
// only the 3MF core is read, and the project has one setup with one bed,
// holding every build item.
// ADR-20261026-w6a records the design.
//
// Read also says what each part prints with: the extruder of each role, the
// painting on its surface, the layer ranges of its object and the support
// its settings ask for, resolved the way the slicer resolves them. Each setup
// lists its filaments with their colours, and each bed its colour and tool
// changes and whether it prints object by object.
//
// Read never trusts the archive. It refuses an archive with too many entries,
// two entries of the same name, or an entry larger than the Limits allow. It
// opens only the entries it knows by name or finds through a relationship, and
// it never writes a name from the archive to a file system.
//
// Thumbnails reads only the pictures, without the model, for a listing that
// shows a picture per file. ReadMesh reads the model parts of one object as
// triangles, made coarser until they fit a bound a browser draws at once.
//
// Rewrite produces a copy of a PrusaSlicer project for a reprint: the
// instances that were not selected are made non-printable, and the chosen bed
// becomes the one the PrusaSlicer command line slices. Every other zip entry
// is copied byte for byte. A project from any other program is
// ErrUnsupported.
//
// Arrange places copies of objects onto as few beds of one setup as hold
// them, by their outlines, and answers one Plate per bed. A Selection with
// Positions writes such a plate: the first position of a build item moves the
// item, and each further one adds a copy of it. The PrusaSlicer command line
// does not arrange a project file, so this is how a program prints an object
// twenty times. ADR-20261027-2rq records the rules.
//
// ParseObjectsInfo and Project.MatchLabels connect a sliced file back to the
// project. The objects_info value a slicer writes into G-code names each
// object the firmware can cancel, with its outline on the bed. MatchLabels
// finds the instance each outline came from. It is made for a PrusaSlicer
// project and a file PrusaSlicer sliced from it.
package threemf
