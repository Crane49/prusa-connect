// Package opt reads the OpenPrintTag database ZIP into plain structs.
package opt

// Limits bound a ZIP so a zip bomb is refused. Total and File count the
// bytes actually read, not what the headers claim.
type Limits struct {
	Entries     int
	Total, File int64
}

//nolint:mnd // the zip limits of the constraints: entries, 64 MiB total, 1 MiB a file.
var DefaultLimits = Limits{Entries: 50_000, Total: 64 << 20, File: 1 << 20}

type Meta struct{ DataHash, LastModified string }

type Brand struct {
	UUID string `json:"uuid"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type Container struct {
	UUID, Slug, BrandSlug, Name string
	EmptyWeightG                *float64
}

type Material struct {
	UUID         string `json:"uuid"`
	Slug         string `json:"slug"`
	BrandSlug    string `json:"brand_slug"`
	Name         string `json:"name"`
	Class        string `json:"class"`
	Type         string `json:"type"`
	Abbreviation string `json:"abbreviation"`
	ColorRGBA    string `json:"color_rgba"`

	Density              *float64 `json:"density,omitempty"`
	HardnessShoreD       *float64 `json:"hardness_shore_d,omitempty"`
	TransmissionDistance *float64 `json:"transmission_distance,omitempty"`

	MinPrintTemp   *int `json:"min_print_temp,omitempty"`
	MaxPrintTemp   *int `json:"max_print_temp,omitempty"`
	PreheatTemp    *int `json:"preheat_temp,omitempty"`
	MinBedTemp     *int `json:"min_bed_temp,omitempty"`
	MaxBedTemp     *int `json:"max_bed_temp,omitempty"`
	ChamberTemp    *int `json:"chamber_temp,omitempty"`
	MinChamberTemp *int `json:"min_chamber_temp,omitempty"`
	MaxChamberTemp *int `json:"max_chamber_temp,omitempty"`
	DryingTemp     *int `json:"drying_temp,omitempty"`
	DryingTimeMin  *int `json:"drying_time_min,omitempty"`

	// Tags are the catalogue's own identifiers, such as abrasive.
	Tags []string `json:"tags,omitempty"`
}

type Package struct {
	UUID          string `json:"uuid"`
	Slug          string `json:"slug"`
	Class         string `json:"class"`
	MaterialSlug  string `json:"material_slug"`
	ContainerSlug string `json:"container_slug"`
	GTIN          string `json:"gtin"`

	NetWeightG *float64 `json:"net_weight_g,omitempty"`
	DiameterUM *int     `json:"diameter_um,omitempty"`
	LengthMM   *int     `json:"length_mm,omitempty"`
}

type Snapshot struct {
	Meta       Meta
	Brands     []Brand
	Containers []Container
	Materials  []Material
	Packages   []Package
	// Skipped counts YAML files that did not parse or had no uuid.
	Skipped int
}
