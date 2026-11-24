package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// seenFile holds, for each printer, when PrusaSlicer last reached it here. It
// sits in the data folder, so the card still says it after a restart.
const seenFile = "seen.json"

// seenEvery is how much later a request must come before the file is written
// again. The card shows minutes, and a slicer asks before every upload.
const seenEvery = time.Minute

// Seen is when PrusaSlicer last reached one printer here. PrusaSlicer holds no
// connection open: it asks on Test and before each upload, so this is the last
// time it asked and not whether it is connected now.
type Seen struct {
	// At is nil for a printer PrusaSlicer has not reached yet.
	At *time.Time `json:"seen_at"`
}

// markSeen records that PrusaSlicer reached the printer now. A failure to
// write only costs the card its time, so it is logged and the request goes on.
func (s *Service) markSeen(printerID string) {
	now := s.now().UTC()

	s.seenMu.Lock()
	defer s.seenMu.Unlock()

	if err := s.loadSeen(); err != nil {
		// The host logs what the module writes to stderr.
		fmt.Fprintf(os.Stderr, "read when PrusaSlicer was last seen: %v\n", err)

		return
	}

	if last, ok := s.seen[printerID]; ok && now.Sub(last) < seenEvery {
		return
	}

	s.seen[printerID] = now

	body, err := json.MarshalIndent(s.seen, "", "  ")
	if err == nil {
		tmp := filepath.Join(s.data, seenFile+".tmp")
		if err = os.WriteFile(tmp, body, 0o600); err == nil {
			err = os.Rename(tmp, filepath.Join(s.data, seenFile))
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "record when PrusaSlicer last reached %s: %v\n", printerID, err)
	}
}

// loadSeen reads seenFile the first time it is needed. The caller holds
// seenMu.
func (s *Service) loadSeen() error {
	if s.seen != nil {
		return nil
	}

	seen := map[string]time.Time{}

	body, err := os.ReadFile(filepath.Join(s.data, seenFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read %s: %w", seenFile, err)
	default:
		if err = json.Unmarshal(body, &seen); err != nil {
			return fmt.Errorf("read %s: %w", seenFile, err)
		}
	}

	s.seen = seen

	return nil
}

// GetSeen answers GET /printers/{printer}/seen: when PrusaSlicer last reached
// the printer here. Whoever sees the printer may read it.
func (s *Service) GetSeen(w http.ResponseWriter, r *http.Request, principal, printerID string) {
	if _, err := s.fleet.Printer(r.Context(), principal, printerID); err != nil {
		pageRefusal(printerID, err).Write(w)

		return
	}

	s.seenMu.Lock()
	err := s.loadSeen()
	last, ok := s.seen[printerID]
	s.seenMu.Unlock()

	if err != nil {
		pageRefusal(printerID, err).Write(w)

		return
	}

	var seen Seen
	if ok {
		seen.At = &last
	}

	writeJSON(w, http.StatusOK, seen)
}
