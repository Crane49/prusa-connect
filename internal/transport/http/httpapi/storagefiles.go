package httpapi

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// The server's own storage. A storage is a declared resource instance, so it is
// addressed by the name an operator gave it under `resources`, and its contents
// are addressed under that name.
//
// storagesPathPrefix is what bodyLimitMiddleware matches to decide which of the
// two body bounds applies, so it is one constant and not a repeated literal.
const (
	storagesPathPrefix     = "/api/v1/storages"
	storagesPath           = storagesPathPrefix
	storageFilesPath       = storagesPathPrefix + "/{storage}/files"
	storageStatPath        = storagesPathPrefix + "/{storage}/stat"
	storageContentPath     = storagesPathPrefix + "/{storage}/content"
	storageFoldersPath     = storagesPathPrefix + "/{storage}/folders"
	storageMovesPath       = storagesPathPrefix + "/{storage}/moves"
	storageDeletionsPath   = storagesPathPrefix + "/{storage}/deletions"
	storageThumbnailPath   = storagesPathPrefix + "/{storage}/thumbnail"
	storageContentFilename = "file"
)

// thumbnailCacheControl lets a browser keep a stored picture but ask the
// server before every use. Several people can share one browser, and the
// server checks read_storage for the person asking each time. A copy that is
// still current costs a 304 with no body, because the answer carries the
// file's digest and modification time.
const thumbnailCacheControl = "private, no-cache"

// Storage is one of the server's own storages: what it is called and what its
// type can actually do.
type Storage struct {
	Name        string `json:"name"         doc:"The name the storage is declared under, which is what every other path here takes."`
	DisplayName string `json:"display_name" doc:"The label the storage's type gives itself."`
	Browse      bool   `json:"browse"       doc:"Whether its entries can be listed and read."`
	Write       bool   `json:"write"        doc:"Whether a file can be written or removed."`
	Move        bool   `json:"move"         doc:"Whether an entry can be renamed inside it. A storage with no rename of its own does not offer one."`
	Ranges      bool   `json:"ranges"       doc:"Whether part of a file can be served without reading it from the start."`
	DownloadURL bool   `json:"download_url" doc:"Whether it hands out a URL a client fetches a file from directly. A download from such a storage is answered with a 307 redirect to that URL instead of this server relaying the bytes."`

	AllowedExtensions []string `json:"allowed_extensions" readOnly:"true" nullable:"false" doc:"The file extensions an upload to this storage may carry, lowercase with the leading dot, such as .gcode. An empty list means any extension is accepted. The server checks the name again on upload, so this is for offering a file picker and a message, not for security."`
}

// StorageEntry is one entry inside a storage.
type StorageEntry struct {
	Name     string    `json:"name"              doc:"The entry's own name, with no directory part."`
	Path     string    `json:"path"              doc:"The path relative to the storage root, \"/\"-separated. It is what every other path here takes."`
	Folder   bool      `json:"folder"            doc:"Whether the entry holds other entries."`
	Size     int64     `json:"size"              doc:"Bytes the entry occupies, zero for a folder."`
	Modified time.Time `json:"modified"          doc:"When the entry last changed, as an RFC 3339 string. The storage reports whole seconds, so this value has no fraction of a second."`
	SHA256   string    `json:"sha256,omitempty"  doc:"The digest the storage took of the file when it was written, lowercase hex. Empty for a folder and for a file the storage kept no digest of."`
	Entries  *int      `json:"entries,omitempty" doc:"How many entries a folder holds directly. Absent for a file and for a folder this server could not list."`
	// Irregular marks a link or a special file. The HTTP listing shows it, and
	// a module's storage.list leaves it out.
	Irregular bool `json:"-"`

	// Print is what the slicer wrote into the file. It is on the stat route and
	// not on the listing: reading it opens the file, and a listing of a
	// thousand rows would open a thousand files. A client asks for it per file
	// it is about to show.
	Print *PrintDetails `json:"print,omitempty" doc:"What the slicer wrote into the file. Absent for a folder, for a format that carries nothing, and for a file this server could not read."`

	// Analysis is what a module that reads this file type said about the
	// file, on the stat route only.
	Analysis *FileAnalysis `json:"analysis,omitempty" doc:"What a module that reads this file type said about the file, on the stat route. Absent when the server read the file itself: no module reads the type, or the module did not answer in time."`
}

// FileAnalysis is what a module said about one file, beside the print
// details its answer also fills.
type FileAnalysis struct {
	Provider string       `json:"provider"          doc:"The namespace of the module that answered."                                                           example:"com.github.aux.acme.reader"`
	Title    string       `json:"title,omitempty"   doc:"What a person calls the file, such as the project name it stores. Absent when the module named none."                                      maxLength:"256"`
	Facts    []FileFact   `json:"facts,omitempty"   doc:"What the file page lists about the file, in order."                                                                                                        maxItems:"64"  nullable:"false"`
	Objects  []FileObject `json:"objects,omitempty" doc:"The parts the file holds, such as the models of a project."                                                                                                maxItems:"256" nullable:"false"`
}

// FileFact is one line a module lists about a file.
type FileFact struct {
	Key   string `json:"key"   doc:"What a program reads the fact by." maxLength:"63"  minLength:"1" example:"plates"`
	Label string `json:"label" doc:"What a person reads the fact by."  maxLength:"256"               example:"Plates"`
	Value string `json:"value" doc:"The value, as text."               maxLength:"256"               example:"3"`
}

// FileObject is one part a file holds.
type FileObject struct {
	ID   string `json:"id"   doc:"The module's id of the part."  maxLength:"63"  minLength:"1"`
	Name string `json:"name" doc:"What a person calls the part." maxLength:"256"`
}

// PrintDetails is what a slicer said about the print inside one file.
//
// Every field is optional and is left out when the file did not say. A zero
// would read as "this print uses no filament", which is not what an absent
// value means.
type PrintDetails struct {
	EstimatedSeconds int64   `json:"estimated_seconds,omitempty" doc:"The print time the slicer estimated for its normal mode, in whole seconds."`
	FilamentMM       float64 `json:"filament_mm,omitempty"       doc:"How much filament the print uses, in millimetres."`
	FilamentG        float64 `json:"filament_g,omitempty"        doc:"How much filament the print uses, in grams."`
	LayerHeightMM    float64 `json:"layer_height_mm,omitempty"   doc:"The layer height the file was sliced at, in millimetres."`
	NozzleMM         float64 `json:"nozzle_mm,omitempty"         doc:"The nozzle diameter the file was sliced for, in millimetres. A multi-tool file reports the first."`
	Material         string  `json:"material,omitempty"          doc:"The filament the file was sliced for, such as PLA. A multi-tool file reports the first."           maxLength:"128"`
	PrinterModel     string  `json:"printer_model,omitempty"     doc:"The printer the file was sliced for, such as MK4IS."                                               maxLength:"128"`
	PrintProfile     string  `json:"print_profile,omitempty"     doc:"The slicer's print profile the file was sliced with, such as 0.20mm STRUCTURAL @MK4IS 0.4."        maxLength:"128"`
	PrinterProfile   string  `json:"printer_profile,omitempty"   doc:"The slicer's printer profile the file was sliced with."                                            maxLength:"128"`
	FilamentProfile  string  `json:"filament_profile,omitempty"  doc:"The slicer's filament profile the file was sliced with. A multi-tool file reports the first."      maxLength:"128"`
	HighFlow         bool    `json:"high_flow,omitempty"         doc:"The file was sliced for a high flow nozzle. A multi-tool file reports the first tool."`

	// ThumbnailWidth and ThumbnailHeight are the size of the picture the
	// thumbnail path will serve, so a page can reserve the space before the
	// picture arrives and the row does not jump when it does.
	ThumbnailWidth  int `json:"thumbnail_width,omitempty"  doc:"The width in pixels of the picture the thumbnail path serves for this file. Absent when the file carries no picture."`
	ThumbnailHeight int `json:"thumbnail_height,omitempty" doc:"The height in pixels of the picture the thumbnail path serves for this file. Absent when the file carries no picture."`

	// Filaments carries what the print needs of each filament. The single
	// values above are the first filament's, kept for a client that knows one
	// only.
	Filaments   []PrintFilament `json:"filaments,omitempty"      doc:"What the print needs of each filament, by the T number that selects it, T0 first. A file sliced for one filament has one entry. The single values above are the first filament's. Which tool of a printer a filament lands on is the printer's tool tree's to say." maxItems:"2048"`
	MaxLayerZMM float64         `json:"max_layer_z_mm,omitempty" doc:"The height of the top layer, in millimetres."`
	Bounds      *PrintBounds    `json:"bounds,omitempty"         doc:"The area the objects cover on the bed. Absent when the file does not say."`
	Bed         *PrintBed       `json:"bed,omitempty"            doc:"The printable volume of the printer profile the file was sliced with. Absent when the file does not say."`
	Checks      *PrintChecks    `json:"checks,omitempty"         doc:"What the start of the G-code asks the printer to verify before it prints. Absent when the G-code could not be read, and an empty object when it was read and asks for nothing."`
	Objects     []PrintObject   `json:"objects,omitempty"        doc:"The objects the slicer labelled, in the order of its objects_info, with the outline of each on the bed. A printer cancels an object by its id. Absent when the file labels none."                                                                                   maxItems:"4096"`
}

// PrintFilament is what a print needs of one filament. Every member is optional and
// is left out when the file did not say.
type PrintFilament struct {
	Index                 int     `json:"index"                               doc:"n of the Tn that selects the filament, counted from 0."`
	Used                  *bool   `json:"used,omitempty"                      doc:"Whether the print extrudes this filament: more than 0.0003 g, the firmware's own threshold. Absent when the file does not say how much of each filament it uses."`
	Material              string  `json:"material,omitempty"                  doc:"The filament type, such as PLA."                                                                                                                                  maxLength:"128"`
	NozzleMM              float64 `json:"nozzle_mm,omitempty"                 doc:"The nozzle diameter the filament was sliced for, in millimetres."`
	HighFlow              *bool   `json:"high_flow,omitempty"                 doc:"Whether the filament was sliced for a high-flow nozzle."`
	Abrasive              *bool   `json:"abrasive,omitempty"                  doc:"Whether the filament wears a nozzle that is not hardened."`
	NozzleTempC           float64 `json:"nozzle_temp_c,omitempty"             doc:"The nozzle temperature, in degrees Celsius."`
	FirstLayerNozzleTempC float64 `json:"first_layer_nozzle_temp_c,omitempty" doc:"The nozzle temperature of the first layer, in degrees Celsius."`
	BedTempC              float64 `json:"bed_temp_c,omitempty"                doc:"The bed temperature the filament asks for, in degrees Celsius."`
	FirstLayerBedTempC    float64 `json:"first_layer_bed_temp_c,omitempty"    doc:"The bed temperature of the first layer, in degrees Celsius."`
	FilamentG             float64 `json:"filament_g,omitempty"                doc:"How much of this filament the print uses, in grams."`
	FilamentMM            float64 `json:"filament_mm,omitempty"               doc:"How much of this filament the print uses, in millimetres."`
	Colour                string  `json:"colour,omitempty"                    doc:"The colour the slicer shows the filament in, as #RRGGBB."                                                                                                         maxLength:"7"`
}

// PrintObject is one object a sliced file labels.
type PrintObject struct {
	ID      int          `json:"id"      doc:"The object's number in objects_info, which M486 S and a printer's cancel list use."`
	Name    string       `json:"name"    doc:"The label the slicer gave the object, such as Cube (Instance 2). It is not unique."      maxLength:"256"`
	Outline []PrintPoint `json:"outline" doc:"The object's outline on the bed it was sliced on, in millimetres from the bed's origin."                 maxItems:"1024"`
}

// PrintPoint is one point on the bed, in millimetres from its origin.
type PrintPoint struct {
	X float64 `json:"x" doc:"Millimetres from the bed's origin along its width."`
	Y float64 `json:"y" doc:"Millimetres from the bed's origin along its depth."`
}

// PrintBounds is a rectangle on the bed.
type PrintBounds struct {
	MinXMM float64 `json:"min_x_mm" doc:"The left edge, in millimetres from the bed's origin."`
	MinYMM float64 `json:"min_y_mm" doc:"The front edge, in millimetres from the bed's origin."`
	MaxXMM float64 `json:"max_x_mm" doc:"The right edge, in millimetres from the bed's origin."`
	MaxYMM float64 `json:"max_y_mm" doc:"The back edge, in millimetres from the bed's origin."`
}

// PrintBed is a printable volume. A member the file did not give is left
// out.
type PrintBed struct {
	WidthMM  float64 `json:"width_mm,omitempty"  doc:"The width of the bed, in millimetres."`
	DepthMM  float64 `json:"depth_mm,omitempty"  doc:"The depth of the bed, in millimetres."`
	HeightMM float64 `json:"height_mm,omitempty" doc:"The highest a print may reach, in millimetres."`
}

// PrintChecks is what the M862 and M115 lines at the start of the G-code ask
// the printer to verify.
type PrintChecks struct {
	PrinterModel string             `json:"printer_model,omitempty" doc:"The model an M862.3 line names, such as MK4S."                                     maxLength:"64"`
	PrinterCode  int                `json:"printer_code,omitempty"  doc:"The numeric model code an M862.2 line names."`
	Nozzles      []PrintNozzleCheck `json:"nozzles,omitempty"       doc:"What each M862.1 line asks of a nozzle."                                                          maxItems:"16"`
	MinFirmware  string             `json:"min_firmware,omitempty"  doc:"The lowest firmware an M862.4 line accepts."                                       maxLength:"64"`
	GCodeLevel   int                `json:"gcode_level,omitempty"   doc:"The G-code level an M862.5 line names."`
	Features     []string           `json:"features,omitempty"      doc:"The firmware features M862.6 lines name, such as Input shaper and MMU3."                          maxItems:"16"`
	Firmware     string             `json:"firmware,omitempty"      doc:"The version an M115 U line names: the newest firmware the slicer's profile knows." maxLength:"64"`
}

// PrintNozzleCheck is one M862.1 line.
type PrintNozzleCheck struct {
	Index      int     `json:"index"                 doc:"n of the Tn the line names, counted from 0. A line that names no T is about T0."`
	DiameterMM float64 `json:"diameter_mm,omitempty" doc:"The nozzle diameter the line asks for, in millimetres."`
	Hardened   *bool   `json:"hardened,omitempty"    doc:"Whether the line asks for a hardened nozzle. Absent when the line does not say."`
	HighFlow   *bool   `json:"high_flow,omitempty"   doc:"Whether the line asks for a high-flow nozzle. Absent when the line does not say."`
}

// FileVersion identifies the stored content of one file, for a conditional
// request. A 304 is decided from it, and it costs no read of the file's
// contents.
type FileVersion struct {
	// Modified is when the file was last written, truncated to the second the
	// HTTP date format carries.
	Modified time.Time
	// Version is the opaque validator the entity tag is built from.
	Version string
}

// FileExtent is how long one file is and whether the path names a directory.
//
// It is what a caller that walks a file in pieces needs and all it needs. Stat
// answers the same two facts, and to fill StorageEntry.Print it also opens the
// file and parses the head of it, which such a caller pays for once per piece
// and never reads.
type FileExtent struct {
	// Size is the file's length in bytes, and zero for a directory.
	Size int64
	// Folder says the path names a directory, which has no contents to read.
	Folder bool
}

// StorageThumbnail is one picture stored inside a file, and what a cache and a
// conditional request are answered from.
type StorageThumbnail struct {
	// Bytes is the encoded picture.
	Bytes []byte
	// MediaType is what those bytes are: image/png or image/jpeg.
	MediaType string
	// Modified is when the file was last written, which is the Last-Modified a
	// conditional request is compared against.
	Modified time.Time
	// Version is the opaque validator the entity tag is built from. It is the
	// storage's digest of the file when it kept one, and the length and the
	// modification time when it did not.
	Version string
}

// SaveResult is what one completed upload produced. The digest is the storage's
// own, taken from the bytes as they were written, so a client that hashed what
// it sent can compare two independent readings.
type SaveResult struct {
	Path   string
	Size   int64
	SHA256 string
	// Temporary says the file is a temporary copy, which the server deletes
	// once it is sent to a printer.
	Temporary bool
}

// Object is one file a caller is about to read, described before its bytes are
// served.
type Object struct {
	// Name is what the file is called, for the download's filename.
	Name string
	// Modified is what a conditional request and a Last-Modified header are
	// answered from.
	Modified time.Time
	// Content is the bytes. The caller closes it.
	Content io.ReadSeekCloser
}

// Storages is the server's own storage, as the transport needs it.
//
// Every method takes the principal, because every one of them is authorized:
// reading a storage's contents and changing them are separate permissions from
// declaring the instance, which is what `resources` covers.
type Storages interface {
	// List returns the storages that are declared and running. A declared
	// storage that failed to start is not offered, because every operation on it
	// would be refused.
	List(ctx context.Context, principal string) ([]Storage, error)

	// Files returns the entries directly inside one directory. The empty path is
	// the storage's own root.
	Files(ctx context.Context, principal string, storage string, path string) ([]StorageEntry, error)

	// Stat describes one entry. For a file a slicer wrote it also opens the
	// file and reads a bounded part of it, which is where StorageEntry.Print
	// comes from.
	Stat(ctx context.Context, principal string, storage string, path string) (StorageEntry, error)

	// Open returns one file for reading. The caller closes Object.Content.
	Open(ctx context.Context, principal string, storage string, path string) (Object, error)

	// DownloadURL returns a URL a client fetches one file from directly, for a
	// storage that hands one out. ok is false when it does not, and the file
	// is read through Open instead. It takes the permission Open takes.
	DownloadURL(ctx context.Context, principal string, storage string, path string) (string, bool, error)

	// FileExtent is one file's length and whether the path names a directory,
	// read without opening the file. It takes the same permission Stat takes.
	FileExtent(
		ctx context.Context, principal string, storage string, path string,
	) (FileExtent, error)

	// FileVersion is what a conditional request on one file is compared
	// against, read without opening the file. It takes the same permission as
	// reading the file.
	FileVersion(
		ctx context.Context, principal string, storage string, path string,
	) (FileVersion, error)

	// Thumbnail returns the picture the slicer stored inside one file. A file
	// that carries none reports ErrNoThumbnail, which the route answers 404
	// with. It takes the same permission as reading the file, because the
	// picture is part of the file.
	Thumbnail(
		ctx context.Context, principal string, storage string, path string,
	) (StorageThumbnail, error)

	// Save writes body into the storage under path and reports what landed. It
	// reads body as it goes and never holds it.
	Save(
		ctx context.Context, principal string, storage string, path string, body io.Reader,
	) (SaveResult, error)

	// SaveTemporary writes body as a temporary copy: a file on its way to a
	// printer, which the server deletes once it is sent. The server chooses
	// where it lands and name gives only the file's own name. It takes the
	// permission Save takes.
	SaveTemporary(
		ctx context.Context, principal string, storage string, name string, body io.Reader,
	) (SaveResult, error)

	// Delete removes one entry, and what is under it when it is a folder.
	Delete(ctx context.Context, principal string, storage string, path string) error

	// DeleteMany removes several entries in one call. Every path is tried, the
	// ones removed stay removed, and a batch where any entry stayed reports
	// core.storage/DELETE_INCOMPLETE. It takes the permission Delete takes.
	DeleteMany(ctx context.Context, principal string, storage string, paths []string) error

	// CreateFolder makes a folder and its parents.
	CreateFolder(ctx context.Context, principal string, storage string, path string) error

	// Move renames an entry inside one storage. It does not cross storages: two
	// storages share no namespace and no atomic step between them.
	Move(ctx context.Context, principal string, storage string, from string, to string) error
}

// storageErrors is what a storage operation answers with. 404 is a storage or a
// path that is not there, 400 a path the storage will not take, 409 a type that
// cannot do it, 413 a body over the bound and 415 an extension the storage
// refuses.
var storageErrors = []int{
	http.StatusBadRequest,
	http.StatusForbidden,
	http.StatusNotFound,
	http.StatusConflict,
	http.StatusRequestEntityTooLarge,
	http.StatusUnsupportedMediaType,
}

// StorageRef is the storage a path-bearing operation acts on. It is exported
// because Huma populates it by reflection, and an unexported embedded struct is
// not reachable that way: the parameter silently arrives empty.
type StorageRef struct {
	Storage string `path:"storage" doc:"The name the storage is declared under." maxLength:"128" example:"local"`
}

type storagePathInput struct {
	StorageRef

	Path string `query:"path" doc:"The entry, relative to the storage root. Empty is the root itself." maxLength:"1024" example:"models"`
}

type requiredPathInput struct {
	StorageRef

	Path string `query:"path" required:"true" doc:"The entry, relative to the storage root." maxLength:"1024" example:"models/bracket.bgcode"`
}

type listStoragesBody struct {
	Body struct {
		Items []Storage `json:"items"`
	}
}

type listStorageFilesBody struct {
	Body struct {
		Items []StorageEntry `json:"items"`
	}
}

type savedObjectBody struct {
	Body struct {
		Path      string `json:"path"   doc:"Where the file landed."`
		Bytes     int64  `json:"bytes"  doc:"How many bytes were written."`
		SHA256    string `json:"sha256,omitempty" doc:"The digest the storage took of the bytes it wrote, lowercase hex. Empty for a storage that keeps no digest."`
		Temporary bool   `json:"temporary,omitempty" doc:"The file is a temporary copy, asked for with temporary=true. Send it to a printer with path as the source. The server deletes it once the file has landed or the transfer was stopped, and one hour after it was written when the send failed, so the same copy can be sent again until then. It is not listed with the storage's files."`
	}
}

type storageEntryBody struct {
	Body StorageEntry
}

// registerStorages publishes the server's own storage: what there is, what is
// in it, and the four ways its contents change.
//
// The upload and the download are the two routes here that do not marshal a
// struct. Both are declared by hand so the document still describes them, and
// neither holds the file: the upload reads the request body as it writes, and
// the download hands the open object to http.ServeContent.
func registerStorages(api huma.API, stores Storages) {
	registerListStorages2(api, stores)
	registerListStorageFiles(api, stores)
	registerStatStorageEntry(api, stores)
	registerDownloadObject(api, stores)
	registerUploadObject(api, stores)
	registerPostObject(api, stores)
	registerDeleteObject(api, stores)
	registerDeleteStorageEntries(api, stores)
	registerCreateStorageFolder(api, stores)
	registerMoveStorageEntry(api, stores)
	registerStorageThumbnail(api, stores)
}

// thumbnailInput is one file's picture, and what the client already holds of
// it.
// StorageRef is embedded rather than requiredPathInput, and Path is repeated
// here, because Huma binds parameters by reflection and cannot reach through an
// unexported embedded struct: the path would arrive empty on every request.
type thumbnailInput struct {
	StorageRef

	Path string `query:"path" required:"true" doc:"The file, relative to the storage root." maxLength:"1024" example:"models/bracket.bgcode"`

	IfNoneMatch     string `header:"If-None-Match"     doc:"The entity tag of the picture this client already has. A picture with the same tag answers 304 with no body."`
	IfModifiedSince string `header:"If-Modified-Since" doc:"The Last-Modified of the picture this client already has. A file no newer than it answers 304 with no body."`
}

// registerStorageThumbnail serves the picture stored inside a file.
//
// The body is the image and not a document, so the response is written by hand,
// the way the camera snapshot is. It carries the conditional-GET headers for
// the same reason that one does: a folder of two hundred models is two hundred
// pictures, and a page that reopens then costs two hundred empty responses
// and no image.
func registerStorageThumbnail(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "get-storage-thumbnail",
		Method:      http.MethodGet,
		Path:        storageThumbnailPath,
		Summary:     "The picture stored inside one file",
		Description: "Serves the thumbnail the slicer wrote into a .gcode, .bgcode or .3mf file, as " +
			"image/png or image/jpeg. It carries Last-Modified and an ETag taken from the storage's " +
			"digest of the file, and a client that sends either back is answered 304 with no body " +
			"while its copy is current. Cache-Control is private, no-cache, so a shared proxy never holds " +
			"one caller's picture, and a browser shared by several people asks before every use, which " +
			"checks read_storage for the person asking. A file that carries no picture answers 404 " +
			"with core.storage/NO_THUMBNAIL. It takes read_storage, the same permission as reading " +
			"the file, because the picture is part of the file.",
		Tags:      []string{"storages"},
		Errors:    storageErrors,
		Responses: thumbnailResponses("The storage's digest of the file."),
	}, func(ctx context.Context, in *thumbnailInput) (*huma.StreamResponse, error) {
		who := principalFrom(ctx)

		// The validators come from the file's own record, which costs a stat.
		// A client that already holds this picture is answered from them alone,
		// so a page reopening a folder of two hundred models parses none of
		// them again.
		version, err := stores.FileVersion(ctx, who, in.Storage, in.Path)
		if err != nil {
			return nil, err
		}

		stamp := version.Modified.UTC().Truncate(time.Second)
		tag := thumbnailTag(version.Version)

		if thumbnailIsCurrent(tag, stamp, in.IfNoneMatch, in.IfModifiedSince) {
			return &huma.StreamResponse{Body: func(hctx huma.Context) {
				writeThumbnailHeaders(hctx, stamp, tag)
				hctx.SetStatus(http.StatusNotModified)
			}}, nil
		}

		picture, err := stores.Thumbnail(ctx, who, in.Storage, in.Path)
		if err != nil {
			return nil, err
		}

		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			writeThumbnail(hctx, picture, stamp, tag)
		}}, nil
	})
}

// thumbnailTag renders the entity tag, or the empty string for a validator that
// cannot be one.
func thumbnailTag(version string) string {
	if !etagSafe(version) {
		return ""
	}

	return etagOf(version)
}

// writeThumbnailHeaders writes what every answer on this route carries, whether
// it holds a picture or says the client's copy is current.
//
// nosniff and the policy are needed. The bytes came out of a file somebody
// uploaded and are served back from this server's origin. A browser that
// guessed a type of its own, or that ran content hidden in a picture format,
// would run the uploader's code on this origin. The parser refuses anything it
// cannot decode as a PNG or a JPEG, and these two headers cover the rest:
// nothing is loaded, and nothing runs.
func writeThumbnailHeaders(hctx huma.Context, stamp time.Time, tag string) {
	hctx.SetHeader("X-Content-Type-Options", "nosniff")
	hctx.SetHeader("Content-Security-Policy", thumbnailPolicy)
	hctx.SetHeader("Cache-Control", thumbnailCacheControl)
	hctx.SetHeader("Last-Modified", stamp.Format(http.TimeFormat))

	if tag != "" {
		hctx.SetHeader("ETag", tag)
	}
}

// thumbnailPolicy loads nothing and runs nothing. A picture needs no script, no
// style and no other document, so the answer says so.
const thumbnailPolicy = "default-src 'none'; sandbox"

// writeThumbnail renders one stored picture.
func writeThumbnail(hctx huma.Context, picture StorageThumbnail, stamp time.Time, tag string) {
	writeThumbnailHeaders(hctx, stamp, tag)

	hctx.SetHeader("Content-Type", picture.MediaType)
	hctx.SetHeader("Content-Length", strconv.Itoa(len(picture.Bytes)))
	hctx.SetStatus(http.StatusOK)

	_, _ = hctx.BodyWriter().Write(picture.Bytes)
}

// thumbnailIsCurrent reports whether the client's copy still matches.
//
// RFC 9110 section 13.1.3 gives If-None-Match precedence: a client that sent an
// entity tag is answered from the tag alone, and the date is read only when
// there was no tag. A tag or a date the server cannot parse is treated as one
// that was not sent, which the same section requires.
func thumbnailIsCurrent(tag string, modified time.Time, ifNoneMatch, ifModifiedSince string) bool {
	if strings.TrimSpace(ifNoneMatch) != "" {
		tags, wildcard, ok := parseTagList(ifNoneMatch)
		if !ok {
			return false
		}

		if wildcard {
			return true
		}

		return tag != "" && weakMatch(tags, trimQuotes(tag))
	}

	if ifModifiedSince == "" {
		return false
	}

	since, err := http.ParseTime(ifModifiedSince)
	if err != nil {
		return false
	}

	return !modified.After(since.UTC())
}

// trimQuotes takes the opaque value back out of a rendered entity tag, which is
// the form the comparison functions are defined over.
func trimQuotes(tag string) string { return strings.Trim(tag, `"`) }

// thumbnailResponses documents what this operation writes. Huma derives
// responses from the returned struct, and a raw body has none to derive. etag
// says what the entity tag is taken from, which is where the two thumbnail
// routes differ.
func thumbnailResponses(etag string) map[string]*huma.Response {
	picture := map[string]*huma.MediaType{
		"image/png":  {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
		"image/jpeg": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
	}

	return map[string]*huma.Response{
		strconv.Itoa(http.StatusOK): {
			Description: "The picture stored inside the file.",
			Content:     picture,
			Headers: map[string]*huma.Param{
				"ETag": {
					Description: etag + " Send it back in If-None-Match.",
				},
				"Last-Modified": {
					Description: "When the file was last written. Send it back in If-Modified-Since.",
				},
				"Cache-Control": {Description: "private, no-cache: the client asks again before every use."},
			},
		},
		strconv.Itoa(http.StatusNotModified): {
			Description: "The client's copy is still current. No body is sent.",
		},
	}
}

func registerListStorages2(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "list-storages",
		Method:      http.MethodGet,
		Path:        storagesPath,
		Summary:     "The server's own storages",
		Description: "The storages an operator declared under `resources` that are running now. Each " +
			"one reports what its type can do, so a caller does not offer an operation that cannot " +
			"work. It takes read_storage.",
		Tags:   []string{"storages"},
		Errors: storageErrors,
	}, func(ctx context.Context, _ *struct{}) (*listStoragesBody, error) {
		items, err := stores.List(ctx, principalFrom(ctx))
		if err != nil {
			return nil, err
		}

		out := &listStoragesBody{}
		out.Body.Items = items

		return out, nil
	})
}

func registerListStorageFiles(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "list-storage-files",
		Method:      http.MethodGet,
		Path:        storageFilesPath,
		Summary:     "One directory of a storage",
		Description: "The entries directly inside a directory. An empty path lists the storage's own " +
			"root. A path carrying a .. segment is refused. It takes read_storage.",
		Tags:   []string{"storages"},
		Errors: storageErrors,
	}, func(ctx context.Context, in *storagePathInput) (*listStorageFilesBody, error) {
		items, err := stores.Files(ctx, principalFrom(ctx), in.Storage, in.Path)
		if err != nil {
			return nil, err
		}

		out := &listStorageFilesBody{}
		out.Body.Items = items

		return out, nil
	})
}

// registerStatStorageEntry describes one entry. It is how a caller checks a
// file it uploaded: the digest the storage took on the write is on the row, so
// the check costs one request and not a whole download.
//
// For a file a slicer wrote it also opens the file and reads a bounded part of
// it, which is where the print object comes from.
func registerStatStorageEntry(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "stat-storage-entry",
		Method:      http.MethodGet,
		Path:        storageStatPath,
		Summary:     "Describe one entry of a storage",
		Description: "Name, size, modification time and, for a storage that keeps one, the SHA-256 " +
			"the storage took when the file was written. For a print file it also carries a print " +
			"object, which the server reads out of the file: a bounded part of the file is opened to " +
			"answer, so a client asks per file it is about to show rather than per listing. It takes " +
			"read_storage.",
		Tags:   []string{"storages"},
		Errors: storageErrors,
	}, func(ctx context.Context, in *requiredPathInput) (*storageEntryBody, error) {
		entry, err := stores.Stat(ctx, principalFrom(ctx), in.Storage, in.Path)
		if err != nil {
			return nil, err
		}

		return &storageEntryBody{Body: entry}, nil
	})
}

// registerDownloadObject serves a file's bytes.
//
// The response is declared by hand and written by http.ServeContent, which
// answers a Range header: the standard library serves a client that resumes
// an interrupted download. The object is opened before the stream starts, so
// a failure to open is still a problem document and not a truncated body.
func registerDownloadObject(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "download-storage-object",
		Method:      http.MethodGet,
		Path:        storageContentPath,
		Summary:     "Download a file from a storage",
		Description: "Serves the file's bytes, with a length and a modification time, and answers a " +
			"Range request so an interrupted download resumes. A storage whose download_url is true " +
			"serves the file itself: the answer is a 307 redirect to a URL that storage handed out, " +
			"and a client follows it to read the bytes. It takes read_storage.",
		Tags:      []string{"storages"},
		Errors:    storageErrors,
		Responses: objectResponses(),
		Metadata:  streams(),
	}, func(ctx context.Context, in *requiredPathInput) (*huma.StreamResponse, error) {
		location, presigned, err := stores.DownloadURL(ctx, principalFrom(ctx), in.Storage, in.Path)
		if err != nil {
			return nil, err
		}

		if presigned {
			return &huma.StreamResponse{Body: func(hctx huma.Context) {
				// A storage's URL may expire, so no cache keeps the redirect
				// past the moment it was asked for.
				hctx.SetHeader("Cache-Control", "no-store")
				hctx.SetHeader("Location", location)
				hctx.SetStatus(http.StatusTemporaryRedirect)
			}}, nil
		}

		object, err := stores.Open(ctx, principalFrom(ctx), in.Storage, in.Path)
		if err != nil {
			return nil, err
		}

		request, writer := requestFrom(ctx), writerFrom(ctx)
		if request == nil || writer == nil {
			_ = object.Content.Close()

			return nil, ErrNoConnectionControl
		}

		return &huma.StreamResponse{Body: func(huma.Context) {
			defer func() { _ = object.Content.Close() }()

			serveDownload(writer, request, object.Name, object.Modified, object.Content)
		}}, nil
	})
}

// objectResponses describes what the download writes, which Huma cannot infer
// from a stream. 206 is here because ServeContent answers a Range with one.
func objectResponses() map[string]*huma.Response {
	octet := map[string]*huma.MediaType{
		"application/octet-stream": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
	}

	return map[string]*huma.Response{
		"200": {Description: "The file's bytes.", Content: octet},
		"206": {Description: "The requested range of the file's bytes.", Content: octet},
		"307": {
			Description: "The storage serves the file itself. No body is sent.",
			Headers: map[string]*huma.Param{
				"Location": {
					Description: "The URL the storage handed out for the file. It may stop working " +
						"after a while, so a client asks this route again rather than keeping it.",
				},
			},
		},
	}
}

// registerUploadObject takes a file into a storage.
//
// The input type declares no body at all, which is what stops Huma reading one:
// the handler reads the request as it writes, so a file of any size costs one
// buffer. The body is described by hand so the document still says what the
// route takes.
//
// It is registered twice, once for PUT and once for POST, because a client
// uploading to a named path does the same thing with either verb, and
// refusing one of the two would tell the client nothing useful. The two
// operations differ only in their id.
func registerUploadObject(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "upload-storage-object",
		Method:      http.MethodPut,
		Path:        storageContentPath,
		Summary:     "Upload a file into a storage",
		Description: uploadDescription,
		Tags:        []string{"storages"},
		Errors:      storageErrors,
		RequestBody: objectRequestBody(),
		Metadata:    streams(),
	}, uploadHandler(stores))
}

// registerPostObject is the same upload under POST. The two operations differ
// only in their id.
func registerPostObject(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "post-storage-object",
		Method:      http.MethodPost,
		Path:        storageContentPath,
		Summary:     "Upload a file into a storage, as a POST",
		Description: uploadDescription,
		Tags:        []string{"storages"},
		Errors:      storageErrors,
		RequestBody: objectRequestBody(),
		Metadata:    streams(),
	}, uploadHandler(stores))
}

// uploadDescription is shared, so the PUT and the POST carry one description.
const uploadDescription = "Writes the request body into the storage at the given path, creating " +
	"parent directories and replacing any file already there. The storage's type decides what " +
	"extensions it accepts. The file is streamed: nothing here holds it. The answer carries the " +
	"SHA-256 the storage took of the bytes it wrote, so a client that hashed what it sent can " +
	"check the two agree. The same upload is published under PUT and under POST. It takes " +
	"write_storage."

// uploadObjectInput is the upload's path and whether the file is a temporary
// copy.
//
// It spells the path out rather than embedding requiredPathInput, because the
// check for unknown query parameters reads one level of embedding and no more.
type uploadObjectInput struct {
	StorageRef

	Path string `query:"path" required:"true" doc:"The entry, relative to the storage root." maxLength:"1024" example:"models/bracket.bgcode"`

	Temporary bool `query:"temporary" example:"true" doc:"Write the file as a temporary copy: a file on its way to a printer that is not kept. The server puts it in a folder of its own and answers where, and only the last part of path, the file's name, is used. A file uploaded without it is never deleted by the server."`
}

// uploadHandler is the body both spellings run.
func uploadHandler(stores Storages) func(context.Context, *uploadObjectInput) (*savedObjectBody, error) {
	return func(ctx context.Context, in *uploadObjectInput) (*savedObjectBody, error) {
		body, err := uploadBody(ctx)
		if err != nil {
			return nil, err
		}

		save := stores.Save
		if in.Temporary {
			save = stores.SaveTemporary
		}

		saved, err := save(ctx, principalFrom(ctx), in.Storage, in.Path, body)
		if err != nil {
			return nil, err
		}

		out := &savedObjectBody{}
		out.Body.Path = saved.Path
		out.Body.Bytes = saved.Size
		out.Body.SHA256 = saved.SHA256
		out.Body.Temporary = saved.Temporary

		return out, nil
	}
}

func objectRequestBody() *huma.RequestBody {
	return &huma.RequestBody{
		Description: "The file's bytes, streamed. Nothing on this path holds the file.",
		Required:    true,
		Content: map[string]*huma.MediaType{
			"application/octet-stream": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
		},
	}
}
