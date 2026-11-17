package routes

import (
	"cmp"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/materials/internal/printers"
)

// GET /printers: the printers a person may observe, searched by name and
// read a page at a time.

// printerView is one printer as the picker shows it.
type printerView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Model is the model the printer names itself, empty when unknown.
	Model     string `json:"model"`
	Connected bool   `json:"connected"`
	// State is the printer's state, such as IDLE, and empty when unknown.
	State      string `json:"state"`
	MayOperate bool   `json:"may_operate"`
}

func viewOf(p printers.Printer, operate bool) printerView {
	return printerView{
		ID: p.ID, Name: p.Name, Model: p.Model, Connected: p.Connected, State: p.State, MayOperate: operate,
	}
}

// MaxPrinters is the most printers one page of GET /printers holds.
const MaxPrinters = 100

// printers answers the printers the caller may observe, by name, with
// whether they may operate each. q keeps those whose name, model or id
// holds it, in any case, and id keeps only the printers it names. A page
// that only names a few printers asks for them by id, because every row
// answered costs one operate question to the host. With limit the answer is one page, and next is the
// offset the following page starts at. The printers come from the host's
// listing for the caller, so neither next nor the page's size tells of one
// they may not see, and only the page's rows are asked about operate. A
// listing the host could not answer is refused, to be tried again.
func (s *server) printers(w http.ResponseWriter, r *http.Request, by string) {
	values := r.URL.Query()
	q := strings.ToLower(strings.TrimSpace(values.Get("q")))

	var offset, limit int

	for field, into := range map[string]*int{"offset": &offset, "limit": &limit} {
		written := values.Get(field)
		if written == "" {
			continue
		}

		count, err := strconv.Atoi(written)
		if err != nil || count < 0 {
			refuse(w, InvalidQuery, field+" is a whole number, and it reads "+strconv.Quote(written), nil)

			return
		}

		*into = count
	}

	if limit > MaxPrinters {
		limit = MaxPrinters
	}

	// The client SDK sends a list as one comma-separated value.
	var named []string
	for _, written := range values["id"] {
		named = append(named, strings.Split(written, ",")...)
	}

	visible, err := s.visible(r.Context(), by)
	if err != nil {
		undecided(w, r, err)

		return
	}

	listed := slices.DeleteFunc(s.fleet.Printers(), func(p printers.Printer) bool {
		return !visible[p.ID] || (len(named) > 0 && !slices.Contains(named, p.ID)) ||
			(q != "" && !strings.Contains(strings.ToLower(p.Name+"\n"+p.Model+"\n"+p.ID), q))
	})
	slices.SortStableFunc(listed, func(a, b printers.Printer) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})

	if limit == 0 {
		limit = len(listed)
	}

	from := min(offset, len(listed))
	rows := make([]printerView, 0, limit)

	// Whether the caller may operate is asked only of the page's rows.
	for _, p := range listed[from:min(from+limit, len(listed))] {
		operate, askErr := s.operates.may(r.Context(), by, actionOperate, p.ID)
		if askErr != nil {
			fmt.Fprintf(os.Stderr, "materials: %s %s: %v\n", r.Method, r.URL.Path, askErr)
		}

		rows = append(rows, viewOf(p, operate))
	}

	page := map[string]any{"printers": rows}
	if next := from + limit; next < len(listed) {
		page["next"] = next
	}

	answer(w, http.StatusOK, page)
}
