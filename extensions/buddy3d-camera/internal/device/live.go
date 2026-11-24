package device

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/buddy3d-camera/internal/camproto"
)

// The live view is a WebRTC connection straight from the browser to the
// camera. This module carries the signalling over the camera's Socket.IO
// session and none of the video. The camera always offers: the browser asks,
// the camera answers with an offer and its candidates, and the browser's
// answer and candidates go back.
//
// A module route is one request and one answer, so the camera's candidates
// are collected for a moment and put into the offer, and the browser sends
// its answer once it has every candidate of its own.

const (
	// offerWait is how long the camera has to answer a request with an
	// offer. The route that waits is held to the host's ten seconds.
	offerWait = 6 * time.Second
	// candidateWait is how long the camera's candidates are collected after
	// its offer. On a network with no relay its host candidates come at
	// once.
	candidateWait = 1500 * time.Millisecond
)

// The refusals of the live view.
var (
	ErrNoOffer   = errors.New("device: the camera sent no offer")
	ErrNoSession = errors.New("device: no live view of that camera is open under that id")
)

// liveSession is one browser's live view of one camera.
type liveSession struct {
	id         string
	offer      chan string
	mu         sync.Mutex
	candidates []candidate
}

type candidate struct {
	line string
	mid  string
}

// live holds the open live view of each camera, by camera. A camera ends
// every session when it is told to stop one, so it serves one at a time.
type live struct {
	mu       sync.Mutex
	sessions map[string]*liveSession
}

// StartLive asks the camera for an offer and answers it with the camera's
// candidates in it. A live view already open on the camera is ended first.
func (s *Service) StartLive(ctx context.Context, camera string) (string, string, error) {
	id, err := sessionID()
	if err != nil {
		return "", "", err
	}

	held := &liveSession{id: id, offer: make(chan string, 1)}

	s.live.mu.Lock()
	previous := s.live.sessions[camera]
	s.live.sessions[camera] = held
	s.live.mu.Unlock()

	if previous != nil {
		_ = s.sio.Send(camera, "webrtc", camproto.WebRTCStopMessage(previous.id))
	}

	if err = s.sio.Send(camera, "webrtc", camproto.WebRTCRequestMessage(id, s.store.VideoQuality(camera))); err != nil {
		s.dropLive(camera, id)

		return "", "", err
	}

	waitCtx, cancel := context.WithTimeout(ctx, offerWait)
	defer cancel()

	var offer string

	select {
	case offer = <-held.offer:
	case <-waitCtx.Done():
		s.dropLive(camera, id)

		return "", "", ErrNoOffer
	}

	select {
	case <-time.After(candidateWait):
	case <-ctx.Done():
	}

	held.mu.Lock()
	collected := append([]candidate(nil), held.candidates...)
	held.mu.Unlock()

	return id, withCandidates(offer, collected), nil
}

// AnswerLive hands the camera the browser's answer and then each of its
// candidates. The camera takes an answer with no candidate in it.
func (s *Service) AnswerLive(camera, id, answer string) error {
	if !s.liveOpen(camera, id) {
		return ErrNoSession
	}

	body, candidates := withoutCandidates(answer)

	if err := s.sio.Send(camera, "webrtc", camproto.WebRTCAnswerMessage(id, body)); err != nil {
		return err
	}

	for _, one := range candidates {
		if err := s.sio.Send(camera, "webrtc", camproto.WebRTCCandidateMessage(id, one.line, one.mid)); err != nil {
			return err
		}
	}

	return nil
}

// StopLive ends the live view. Nothing tells the camera a browser went away,
// so a page that closes says so.
func (s *Service) StopLive(camera, id string) error {
	if !s.liveOpen(camera, id) {
		return ErrNoSession
	}

	s.dropLive(camera, id)

	return s.sio.Send(camera, "webrtc", camproto.WebRTCStopMessage(id))
}

// onWebRTC files a camera's offer or candidate under the session it names.
func (s *Service) onWebRTC(camera string, payload []byte) {
	message, err := camproto.DecodeWebRTC(payload)
	if err != nil {
		return
	}

	s.live.mu.Lock()
	held := s.live.sessions[camera]
	s.live.mu.Unlock()

	if held == nil || held.id != message.RequestID {
		return
	}

	switch message.Type {
	case camproto.WebRTCOffer:
		select {
		case held.offer <- message.SDP:
		default:
		}
	case camproto.WebRTCCandidate:
		held.mu.Lock()
		held.candidates = append(held.candidates, candidate{line: message.SDP, mid: message.Mid})
		held.mu.Unlock()
	}
}

func (s *Service) liveOpen(camera, id string) bool {
	s.live.mu.Lock()
	defer s.live.mu.Unlock()

	held := s.live.sessions[camera]

	return held != nil && held.id == id
}

func (s *Service) dropLive(camera, id string) {
	s.live.mu.Lock()
	defer s.live.mu.Unlock()

	if held := s.live.sessions[camera]; held != nil && held.id == id {
		delete(s.live.sessions, camera)
	}
}

// sessionIDBytes is the random bytes of a live view's id, and minSections
// the session header and one media section an offer holds at least.
const (
	sessionIDBytes = 8
	minSections    = 2
)

func sessionID() (string, error) {
	b := make([]byte, sessionIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

// withCandidates puts each candidate line into the media section its mid
// names, or the first one, which a bundled connection uses for all of them.
func withCandidates(sdp string, candidates []candidate) string {
	sections := splitSections(sdp)
	if len(sections) < minSections {
		return sdp
	}

	for _, one := range candidates {
		line := one.line
		if !strings.HasPrefix(line, "a=") {
			line = "a=" + line
		}

		target := 1
		for index := 1; index < len(sections); index++ {
			if one.mid != "" && slices.Contains(sections[index], "a=mid:"+one.mid) {
				target = index

				break
			}
		}

		sections[target] = append(sections[target], line)
	}

	return joinSections(sections)
}

// withoutCandidates takes the candidate lines out of a description and
// answers them with the mid of the media section each was in.
func withoutCandidates(sdp string) (string, []candidate) {
	sections := splitSections(sdp)

	var found []candidate

	for index, section := range sections {
		mid := ""

		for _, line := range section {
			if value, ok := strings.CutPrefix(line, "a=mid:"); ok {
				mid = value
			}
		}

		kept := section[:0]

		for _, line := range section {
			switch {
			case strings.HasPrefix(line, "a=candidate:"):
				found = append(found, candidate{line: line, mid: mid})
			case line == "a=end-of-candidates":
			default:
				kept = append(kept, line)
			}
		}

		sections[index] = kept
	}

	return joinSections(sections), found
}

// splitSections is the session part of a description and then each media
// section, as lines.
func splitSections(sdp string) [][]string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(sdp, "\r\n"), "\r\n", "\n"), "\n")
	sections := [][]string{{}}

	for _, line := range lines {
		if strings.HasPrefix(line, "m=") {
			sections = append(sections, []string{})
		}

		sections[len(sections)-1] = append(sections[len(sections)-1], line)
	}

	return sections
}

func joinSections(sections [][]string) string {
	var out strings.Builder

	for _, section := range sections {
		for _, line := range section {
			out.WriteString(line + "\r\n")
		}
	}

	return out.String()
}
