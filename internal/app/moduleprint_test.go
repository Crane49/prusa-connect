package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// printingModule is the instance every case here prints as.
const printingModule = "sink:queue"

// modulePrinting is a module's fleet view over a plane that can read one
// storage, with the ceiling the operator wrote for the instance.
func modulePrinting(
	t *testing.T, graph *authz.Store, plane *devices, ceiling ...authz.Action,
) moduleFleet {
	t.Helper()

	return moduleFleet{
		fleet:    plane,
		grants:   graph,
		standing: moduleStanding{principal: printingModule, ceiling: ceiling},
	}
}

// planePrinting is a plane over a storage holding one file, with a printer in
// it, and the id of that printer.
func planePrinting(t *testing.T, graph *authz.Store, serial string) (*devices, string) {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "part.gcode"), []byte("G28\n"), 0o600))

	plane := planeReading(t, graph, stageOne(t, root))

	return plane, arrive(t, plane, serial)
}

// fromStorage is the request every case here makes.
var fromStorage = deviceapi.PrintParams{
	Storage: "bench", Source: "part.gcode", Path: "usb/part.gcode",
}

// A module's own print takes operate on the printer and read_storage on the
// storage kind, and the ceiling is held against both. An instance the operator
// installed with operate alone prints a file the machine already holds and
// never reads one out of a storage.
func TestAModulesOwnPrintIsHeldToItsCeilingOnBothActions(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, "SIM-PRINT-CEILING")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, printingModule)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	params := fromStorage
	params.ID = id

	// The refusal. The operator listed operate and not read_storage, so the
	// call is refused before the storage is resolved.
	withoutStorage := modulePrinting(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

	_, err := withoutStorage.Print(t.Context(), params)
	require.ErrorIs(t, err, deviceapi.ErrDenied)

	// The permit. With read_storage in the ceiling the call reaches the file,
	// and what refuses it is the printer: the stand-in session declares no
	// capability.
	withStorage := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	_, err = withStorage.Print(t.Context(), params)
	require.NotErrorIs(t, err, deviceapi.ErrDenied,
		"a module inside its ceiling was refused: %v", err)
}

// A module the graph gave nothing on the storage kind is refused even when its
// ceiling names read_storage. The ceiling is a list of actions and never a
// grant.
func TestAModuleGrantedNothingOnTheStorageKindCannotPrintFromOne(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, "SIM-PRINT-UNGRANTED")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, printingModule)))

	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	params := fromStorage
	params.ID = id

	_, err := fleet.Print(t.Context(), params)
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// A module printing on a machine it holds nothing on is told the printer is
// not there, which is the answer a caller who reaches nothing on it gets
// everywhere else.
func TestAModulePrintingOnAPrinterItCannotOperateIsRefused(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, "SIM-PRINT-UNREACHED")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	params := fromStorage
	params.ID = id

	_, err := fleet.Print(t.Context(), params)
	require.Error(t, err)
	require.ErrorIs(t, err, deviceapi.ErrNoDevice)
}

// A print from a storage answers with the transfer's id while the file is
// still on its way, and with no command id: the start command is sent when the
// last byte has arrived, so there is no id for it yet.
func TestAModulesPrintAnswersWithTheTransferItOpened(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, "SIM-PRINT-STARTED")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, printingModule)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	params := fromStorage
	params.ID = id

	started, err := fleet.Print(t.Context(), params)
	require.NoError(t, err)
	require.NotEmpty(t, started.TransferID,
		"a print from a storage answered without the transfer a module follows it by")
	require.Empty(t, started.CommandID)
}

// A request naming half a source never reaches the plane: the same refusal the
// HTTP route gives, because it is the same check.
func TestAModulesPrintNamingHalfASourceIsRefused(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, "SIM-PRINT-HALF")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, printingModule)))

	require.Error(t, httpapi.ValidatePrint(httpapi.DevicePrint{Storage: "bench", Path: "usb/part.gcode"}))
	require.Error(t, httpapi.ValidatePrint(httpapi.DevicePrint{Source: "part.gcode", Path: "usb/part.gcode"}))
	require.Error(t, httpapi.ValidatePrint(httpapi.DevicePrint{}))

	_, err := plane.StartPrint(t.Context(), printingModule, id,
		httpapi.DevicePrint{Storage: "bench", Path: "usb/part.gcode"})
	require.Error(t, err)
}

// A source that climbs out of its storage is refused, and the refusal comes
// before anything opens a file. It is the same rule the storage routes hold,
// in both spellings of a separator, and it is the answer a file that is not
// there gets: the storage contract joins absence and escape so that neither
// says which names in a store point out of it.
func TestAPrintCannotClimbOutOfItsStorage(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, "SIM-PRINT-CLIMB")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, printingModule)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	for _, source := range []string{"../part.gcode", `..\part.gcode`, "a/../../part.gcode"} {
		params := deviceapi.PrintParams{
			ID: id, Storage: "bench", Source: source, Path: "usb/part.gcode",
		}

		_, err := fleet.Print(t.Context(), params)
		require.ErrorIsf(t, err, deviceapi.ErrNoFile, "%q was not refused", source)
	}
}

// A storage this deployment does not declare, a file it does not hold and a
// source that climbs out of it are one refusal, down to the sentence. A module
// that could tell them apart would read the list of storages out of the
// refusals it collects.
func TestAPrintRefusesEveryAbsentSourceWithOneSentence(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, "SIM-PRINT-ABSENT")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, printingModule)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	refusalOf := func(storage, source string) error {
		t.Helper()

		_, err := fleet.Print(t.Context(), deviceapi.PrintParams{
			ID: id, Storage: storage, Source: source, Path: "usb/part.gcode",
		})
		require.Errorf(t, err, "%s/%s was printed", storage, source)
		require.ErrorIs(t, err, deviceapi.ErrNoFile)

		return err
	}

	missing := refusalOf("bench", "nothing.gcode")
	absent := refusalOf("shelf", "part.gcode")
	climbing := refusalOf("bench", "../part.gcode")

	require.Equal(t, missing.Error(), absent.Error(),
		"the sentence says whether the storage is one this deployment declares")
	require.Equal(t, missing.Error(), climbing.Error(),
		"the sentence says whether the source left the storage")
	require.NotContains(t, missing.Error(), "bench",
		"the refusal names the storage the module asked about")
}

// planeGranted is planePrinting with the module granted operate on the printer
// and read_storage on the storage kind.
func planeGranted(t *testing.T, serial string) (*authz.Store, *devices, string) {
	t.Helper()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, serial)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, printingModule)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	return graph, plane, id
}

// dataHolding is a module's data directory with copy.gcode in its print
// folder, and outside.gcode beside that folder.
func dataHolding(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "print"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "print", "copy.gcode"), []byte("G28\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.gcode"), []byte("G28\n"), 0o600))

	return dir
}

// fromData is a print of source from the calling module's print folder.
func fromData(id, source string) deviceapi.PrintParams {
	return deviceapi.PrintParams{
		ID: id, Storage: deviceapi.ModuleDataStorage, Source: source, Path: "usb/copy.gcode",
	}
}

// A module with a data directory prints a file from its print folder, and
// the print answers with the transfer as a print from a storage does.
func TestAModulePrintsFromItsPrintFolder(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-PRINT-DATA")
	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)
	fleet.data = dataHolding(t)

	started, err := fleet.Print(t.Context(), fromData(id, "copy.gcode"))
	require.NoError(t, err)
	require.NotEmpty(t, started.TransferID)
}

// A print from the print folder takes read_storage, as a print from any
// storage does (ADR-20261023-sns rule 6).
func TestAPrintFromThePrintFolderTakesReadStorage(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-PRINT-DATA-READ")
	fleet := modulePrinting(t, graph, plane, authz.ActionObserve, authz.ActionOperate)
	fleet.data = dataHolding(t)

	_, err := fleet.Print(t.Context(), fromData(id, "copy.gcode"))
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// A module the operator granted no data directory has no print folder, and
// is refused before anything is read.
func TestAModuleWithoutADataDirectoryCannotPrintFromOne(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-PRINT-NODATA")
	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	_, err := fleet.Print(t.Context(), fromData(id, "copy.gcode"))
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// A source that climbs out of the print folder, in either spelling of a
// separator, and a link in it that points out of it are the refusal a
// missing file gets (ADR-20261023-sns rule 8).
func TestAPrintFromThePrintFolderStaysInIt(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-PRINT-DATA-CLIMB")
	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)
	fleet.data = dataHolding(t)

	sources := []string{"../outside.gcode", `..\outside.gcode`, "missing.gcode"}

	link := filepath.Join(fleet.data, "print", "link.gcode")
	if os.Symlink(filepath.Join(fleet.data, "outside.gcode"), link) == nil {
		sources = append(sources, "link.gcode")
	}

	for _, source := range sources {
		_, err := fleet.Print(t.Context(), fromData(id, source))
		require.ErrorIsf(t, err, deviceapi.ErrNoFile, "%q was not refused", source)
	}
}

// A print folder that is itself a link out of the data directory is not the
// module's folder, and the file it reaches is the refusal a missing file gets
// (ADR-20261023-sns rule 8).
func TestAPrintFolderLinkedOutOfTheDataDirectoryIsNoFolder(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-PRINT-DATA-LINK")
	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	dir := t.TempDir()
	fleet.data = filepath.Join(dir, "data")
	require.NoError(t, os.Mkdir(fleet.data, 0o750))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "outside"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside", "copy.gcode"), []byte("G28\n"), 0o600))

	if err := os.Symlink(filepath.Join("..", "outside"), filepath.Join(fleet.data, "print")); err != nil {
		t.Skipf("links cannot be made here: %v", err)
	}

	_, err := fleet.Print(t.Context(), fromData(id, "copy.gcode"))
	require.ErrorIs(t, err, deviceapi.ErrNoFile)
}

// The core only reads a module's data directory (ADR-20261023-sns rule 9). A print
// before the module made its print folder, or after its data directory went,
// is the refusal a missing file gets and leaves both as they were.
func TestAPrintFromAnAbsentPrintFolderCreatesNothing(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-PRINT-DATA-ABSENT")
	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	for _, data := range []string{t.TempDir(), filepath.Join(t.TempDir(), "gone")} {
		fleet.data = data

		_, err := fleet.Print(t.Context(), fromData(id, "x.gcode"))
		require.ErrorIs(t, err, deviceapi.ErrNoFile)
		require.NoFileExists(t, filepath.Join(data, "print"))
		require.NoDirExists(t, filepath.Join(data, "print"))
	}
}

// module:data names the calling module's own folder. A file only another
// module holds is not there for it (ADR-20261023-sns rule 7).
func TestAModulePrintsFromItsOwnFolderOnly(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-PRINT-DATA-OWN")
	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	dataHolding(t) // another module's folder, holding copy.gcode
	fleet.data = t.TempDir()

	_, err := fleet.Print(t.Context(), fromData(id, "copy.gcode"))
	require.ErrorIs(t, err, deviceapi.ErrNoFile)
}

// A print over HTTP that names module:data reads no module's folder. It is
// the refusal a storage this server does not declare gets, even for a caller
// holding both permissions (ADR-20261023-sns rule 9).
func TestAPrintOverHTTPCannotNameModuleData(t *testing.T) {
	t.Parallel()

	_, plane, id := planeGranted(t, "SIM-PRINT-DATA-HTTP")

	_, err := plane.Print(t.Context(), printingModule, id, httpapi.DevicePrint{
		Storage: deviceapi.ModuleDataStorage, Source: "copy.gcode", Path: "usb/copy.gcode",
	})
	require.ErrorIs(t, err, fault.StorageNotFound.New(""))
}

// The storage routes neither list module:data nor browse it (ADR-20261023-sns rule 9).
func TestModuleDataIsNoStorage(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	plane := storagePlane{graph: graph, stores: stores{host: stageOne(t, t.TempDir()).host}}

	listed, err := plane.List(t.Context(), printingModule)
	require.NoError(t, err)

	names := make([]string, 0, len(listed))
	for _, storage := range listed {
		names = append(names, storage.Name)
	}

	require.Equal(t, []string{"bench"}, names)

	_, err = plane.Files(t.Context(), printingModule, deviceapi.ModuleDataStorage, "")
	require.ErrorIs(t, err, fault.StorageNotFound.New(""))
}
