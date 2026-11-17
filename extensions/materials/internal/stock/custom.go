package stock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/prusa3d/materials/internal/colour"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInUse    = errors.New("in use")
	ErrFull     = errors.New("too many spools")
	ErrInvalid  = errors.New("invalid")
)

// MaterialFields is what the owner writes of a custom material.
type MaterialFields struct {
	Name         string   `json:"name"`
	BrandName    string   `json:"brand_name"`
	Class        string   `json:"class"`
	Type         string   `json:"type"`
	Abbreviation string   `json:"abbreviation"`
	ColorRGBA    string   `json:"color_rgba"`
	DiameterUM   *int     `json:"diameter_um,omitempty"`
	Density      *float64 `json:"density,omitempty"`

	MinPrintTemp  *int `json:"min_print_temp,omitempty"`
	MaxPrintTemp  *int `json:"max_print_temp,omitempty"`
	PreheatTemp   *int `json:"preheat_temp,omitempty"`
	MinBedTemp    *int `json:"min_bed_temp,omitempty"`
	MaxBedTemp    *int `json:"max_bed_temp,omitempty"`
	ChamberTemp   *int `json:"chamber_temp,omitempty"`
	DryingTemp    *int `json:"drying_temp,omitempty"`
	DryingTimeMin *int `json:"drying_time_min,omitempty"`

	MinChamberTemp       *int     `json:"min_chamber_temp,omitempty"`
	MaxChamberTemp       *int     `json:"max_chamber_temp,omitempty"`
	MinNozzleDiameter    *float64 `json:"min_nozzle_diameter,omitempty"`
	TransmissionDistance *float64 `json:"transmission_distance,omitempty"`
	HardnessShoreA       *float64 `json:"hardness_shore_a,omitempty"`
	HardnessShoreD       *float64 `json:"hardness_shore_d,omitempty"`

	RALCode string `json:"ral_code"`
	// SecondaryColors are up to five more colours, each as color_rgba is.
	SecondaryColors []string `json:"secondary_colors"`
	// Certifications and Tags are OpenPrintTag identifiers, such as abrasive.
	Certifications []string `json:"certifications"`
	Tags           []string `json:"tags"`
}

// Custom is a material only its owner sees.
type Custom struct {
	MaterialFields

	UUID      string    `json:"uuid"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// customFields are the columns MaterialFields writes, in the order of args.
var customFields = []string{
	"name", "brand_name", "class", "type", "abbreviation", "color_rgba", "diameter_um", "density",
	"min_print_temp", "max_print_temp", "preheat_temp", "min_bed_temp", "max_bed_temp", "chamber_temp",
	"drying_temp", "drying_time_min", "min_chamber_temp", "max_chamber_temp", "min_nozzle_diameter",
	"transmission_distance", "hardness_shore_a", "hardness_shore_d", "ral_code", "secondary_colors",
	"certifications", "tags",
}

var customColumns = strings.Join(customFields, ", ")

// The most colours, and the most certifications or tags, one material lists.
const (
	maxSecondary = 5
	maxLabels    = 64
)

// The most bytes a person may write in one text field, so 100 spools or
// materials fit one answer under the host's 1 MiB.
const (
	maxText  = 200
	maxNotes = 2000
)

// fits is false for text over limit bytes, not UTF-8, or holding a control
// character other than a tab or a line break. JSON then at most doubles it.
func fits(s string, limit int) bool {
	return len(s) <= limit && utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r'
	})
}

// check refuses a blank name, a class outside FFF and SLA, or a text that
// does not fit.
func (f MaterialFields) check() error {
	if strings.TrimSpace(f.Name) == "" || (f.Class != "FFF" && f.Class != "SLA") {
		return fmt.Errorf("custom material needs a name and class FFF or SLA: %w", ErrInvalid)
	}

	for _, text := range []string{f.Name, f.BrandName, f.Type, f.Abbreviation, f.RALCode} {
		if !fits(text, maxText) {
			return fmt.Errorf("a text of a custom material is at most %d bytes of plain text: %w", maxText, ErrInvalid)
		}
	}

	if len(f.SecondaryColors) > maxSecondary || len(f.Certifications) > maxLabels || len(f.Tags) > maxLabels {
		return fmt.Errorf(
			"a custom material lists at most %d colours and %d labels: %w",
			maxSecondary,
			maxLabels,
			ErrInvalid,
		)
	}

	// The colour reaches every viewer's page in a style attribute, so it is a colour or nothing.
	if f.ColorRGBA != "" && !colour.Valid(f.ColorRGBA) {
		return fmt.Errorf("colour %q is not #rrggbb or #rrggbbaa: %w", f.ColorRGBA, ErrInvalid)
	}

	for _, c := range f.SecondaryColors {
		if !colour.Valid(c) {
			return fmt.Errorf("secondary colour %q is not #rrggbb or #rrggbbaa: %w", c, ErrInvalid)
		}
	}

	for _, label := range append(append([]string{}, f.Certifications...), f.Tags...) {
		if !isLabel(label) {
			return fmt.Errorf("label %q is not a lower_snake identifier: %w", label, ErrInvalid)
		}
	}

	return nil
}

// isLabel is true for a lower_snake identifier of at most 64 bytes, which is
// what an OpenPrintTag tag or certification is, and holds no comma.
func isLabel(s string) bool {
	return s != "" && len(s) <= 64 && !strings.ContainsFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_'
	})
}

// joined writes a list column, and split reads one back; an empty column is an
// empty list, never a list of one empty string.
func joined(items []string) string { return strings.Join(items, ",") }

func split(column string) []string {
	if column == "" {
		return []string{}
	}

	return strings.Split(column, ",")
}

// placeholders writes $from, $from+1 … for n values.
func placeholders(from, n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("$%d", from+i)
	}

	return strings.Join(out, ", ")
}

func (f MaterialFields) args() []any {
	return []any{
		f.Name,
		f.BrandName,
		f.Class,
		f.Type,
		f.Abbreviation,
		f.ColorRGBA,
		f.DiameterUM,
		f.Density,
		f.MinPrintTemp,
		f.MaxPrintTemp,
		f.PreheatTemp,
		f.MinBedTemp,
		f.MaxBedTemp,
		f.ChamberTemp,
		f.DryingTemp,
		f.DryingTimeMin,
		f.MinChamberTemp,
		f.MaxChamberTemp,
		f.MinNozzleDiameter,
		f.TransmissionDistance,
		f.HardnessShoreA,
		f.HardnessShoreD,
		f.RALCode,
		joined(f.SecondaryColors),
		joined(f.Certifications),
		joined(f.Tags),
	}
}

// CreateCustom keeps a new custom material for owner.
func (s *Store) CreateCustom(ctx context.Context, owner string, f MaterialFields) (Custom, error) {
	if err := f.check(); err != nil {
		return Custom{}, err
	}

	c := Custom{UUID: uuid.NewString(), MaterialFields: f, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	n := firstField + len(customFields)

	//nolint:gosec // the columns are the constants above.
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO custom_materials (uuid, owner, `+customColumns+`, created_at, updated_at, color_family)
		VALUES ($1, $2, `+placeholders(firstField, len(customFields))+fmt.Sprintf(", $%d, $%d, $%d)", n, n, n+1),
		append(append([]any{c.UUID, owner}, f.args()...), s.at(c.CreatedAt), colour.Family(f.ColorRGBA))...)
	if err != nil {
		return Custom{}, fmt.Errorf("insert custom material: %w", err)
	}

	// Read back, so a list left out answers as an empty one, as later reads do.
	return s.custom(ctx, owner, c.UUID)
}

// firstField is the placeholder of the first of customFields: $1 and $2 are
// the uuid and the owner.
const firstField = 3

// UpdateCustom replaces the fields of the owner's custom material, and the
// fields its live spools copied of it, in one transaction. A deleted spool
// keeps what it was.
func (s *Store) UpdateCustom(ctx context.Context, owner, id string, f MaterialFields) (Custom, error) {
	if err := f.check(); err != nil {
		return Custom{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Custom{}, fmt.Errorf("begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	args := append(append([]any{id, owner}, f.args()...), s.at(now), colour.Family(f.ColorRGBA))

	sets := make([]string, len(customFields))
	for i, column := range customFields {
		sets[i] = fmt.Sprintf("%s=$%d", column, firstField+i)
	}

	n := firstField + len(customFields)

	//nolint:gosec // the columns are the constants above.
	res, err := tx.ExecContext(ctx, `UPDATE custom_materials SET `+strings.Join(sets, ", ")+
		fmt.Sprintf(", updated_at=$%d, color_family=$%d WHERE uuid=$1 AND owner=$2", n, n+1), args...)
	if err != nil {
		return Custom{}, fmt.Errorf("update custom material: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return Custom{}, fmt.Errorf("update custom material: %w", err)
	}

	if rows == 0 {
		return Custom{}, ErrNotFound
	}

	if _, err = tx.ExecContext(ctx, `UPDATE spools SET material_name=$2, brand_name=$3, class=$4, type=$5,
		color_rgba=$6, density=$7, diameter_um=$8, color_family=$9, updated_at=$10
		WHERE material_uuid=$1 AND material_source='custom' AND deleted_at IS NULL`,
		id, f.Name, f.BrandName, f.Class, f.Type, f.ColorRGBA, f.Density, f.DiameterUM, colour.Family(f.ColorRGBA),
		s.at(now)); err != nil {
		return Custom{}, fmt.Errorf("update the spools of a custom material: %w", err)
	}

	tools, err := renameOnTools(ctx, tx, id, f)
	if err != nil {
		return Custom{}, err
	}

	if err = tx.Commit(); err != nil {
		return Custom{}, fmt.Errorf("commit: %w", err)
	}

	for _, t := range tools {
		s.toolChanged(t[0], t[1])
	}

	return s.custom(ctx, owner, id)
}

// renameOnTools copies a custom material's fields onto the tools it is on,
// and answers those tools, each as its device id and tool id.
func renameOnTools(ctx context.Context, tx *sql.Tx, id string, f MaterialFields) ([][2]string, error) {
	rows, err := tx.QueryContext(ctx, `UPDATE assignments SET material_name=$2, brand_name=$3, type=$4,
		abbreviation=$5, color_rgba=$6 WHERE material_uuid=$1 AND material_source='custom'
		RETURNING device_id, tool_id`,
		id, f.Name, f.BrandName, f.Type, f.Abbreviation, f.ColorRGBA)
	if err != nil {
		return nil, fmt.Errorf("update the tools of a custom material: %w", err)
	}

	defer rows.Close()

	var tools [][2]string

	for rows.Next() {
		var t [2]string
		if err = rows.Scan(&t[0], &t[1]); err != nil {
			return nil, fmt.Errorf("read a tool of a custom material: %w", err)
		}

		tools = append(tools, t)
	}

	return tools, rows.Err()
}

// custom reads the owner's custom material.
func (s *Store) custom(ctx context.Context, owner, id string) (Custom, error) {
	var (
		c                       = Custom{UUID: id}
		created, updated        any
		secondary, certs, tagsC string
	)

	err := s.db.QueryRowContext(ctx, `SELECT `+customColumns+`, created_at, updated_at FROM custom_materials
		WHERE uuid=$1 AND owner=$2`, id, owner).
		Scan(&c.Name, &c.BrandName, &c.Class, &c.Type, &c.Abbreviation, &c.ColorRGBA, &c.DiameterUM, &c.Density,
			&c.MinPrintTemp, &c.MaxPrintTemp, &c.PreheatTemp, &c.MinBedTemp, &c.MaxBedTemp, &c.ChamberTemp,
			&c.DryingTemp, &c.DryingTimeMin, &c.MinChamberTemp, &c.MaxChamberTemp, &c.MinNozzleDiameter,
			&c.TransmissionDistance, &c.HardnessShoreA, &c.HardnessShoreD, &c.RALCode, &secondary, &certs, &tagsC,
			&created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Custom{}, ErrNotFound
	}

	if err != nil {
		return Custom{}, fmt.Errorf("read custom material: %w", err)
	}

	c.SecondaryColors, c.Certifications, c.Tags = split(secondary), split(certs), split(tagsC)

	if c.CreatedAt, err = parseTime(created); err != nil {
		return Custom{}, err
	}

	c.UpdatedAt, err = parseTime(updated)

	return c, err
}

// DeleteCustom removes the owner's custom material and its favourites, unless a live spool still has it.
func (s *Store) DeleteCustom(ctx context.Context, owner, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM custom_materials WHERE uuid=$1 AND owner=$2`, id, owner).
		Scan(&n); err != nil {
		return fmt.Errorf("find custom material: %w", err)
	}

	if n == 0 {
		return ErrNotFound
	}

	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM spools WHERE material_uuid=$1 AND deleted_at IS NULL`, id).
		Scan(&n); err != nil {
		return fmt.Errorf("count spools: %w", err)
	}

	if n > 0 {
		return ErrInUse
	}

	for _, q := range []string{`DELETE FROM favourites WHERE material_uuid=$1`, `DELETE FROM custom_materials WHERE uuid=$1`} {
		if _, err = tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("delete custom material: %w", err)
		}
	}

	return tx.Commit()
}
