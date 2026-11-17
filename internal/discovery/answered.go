package discovery

import "time"

// answered is the record of recent answers: when each record last went to
// each place. Entries leave in the order they came, once they are older than
// answerInterval or once maxSent newer ones follow them. Each note costs the
// same whatever the record holds.
type answered struct {
	at    stamps
	order []sentAt
	head  int
	// touched counts the entries of the queue a note read or moved. With
	// the count the stamps keep, it is what a test holds the cost to.
	touched int
}

// sentAt is one entry of the record, in the order the entries came.
type sentAt struct {
	key string
	at  time.Time
}

// newAnswered is an empty record.
func newAnswered() answered {
	return answered{at: stamps{by: make(map[string]time.Time)}}
}

// last is when a record last went to a place.
func (s *answered) last(key string) (time.Time, bool) {
	return s.at.get(key)
}

// note records one answer, after forgetting the ones too old to matter and,
// when the record is full, the oldest one.
func (s *answered) note(key string, now time.Time) {
	for s.head < len(s.order) && now.Sub(s.order[s.head].at) >= answerInterval {
		s.forget()
	}

	if len(s.order)-s.head >= maxSent {
		s.forget()
	}

	// The queue is moved to the front of its array once half of it is
	// forgotten, so it stays within twice its limit. That move is paid for
	// by the maxSent notes before it.
	if s.head > maxSent {
		s.touched += len(s.order) - s.head
		s.order = append(s.order[:0], s.order[s.head:]...)
		s.head = 0
	}

	s.at.put(key, now)
	s.order = append(s.order, sentAt{key: key, at: now})
}

// forget drops the oldest entry. The stamps keep a key noted again since.
func (s *answered) forget() {
	s.touched++

	oldest := s.order[s.head]
	s.order[s.head] = sentAt{}
	s.head++

	if at, ok := s.at.get(oldest.key); ok && at.Equal(oldest.at) {
		s.at.drop(oldest.key)
	}
}

// size is how many entries the record holds.
func (s *answered) size() int {
	return len(s.order) - s.head
}

// cost is every entry a note touched so far, in the queue and in the stamps.
func (s *answered) cost() int {
	return s.touched + s.at.touched
}

// reset forgets every answer.
func (s *answered) reset() {
	s.at.each(func(key string, _ time.Time) { s.at.drop(key) })
	s.order, s.head = s.order[:0], 0
}

// stamps is when each key was last noted. Every read, write and walk goes
// through its methods, which count the entries they touch.
type stamps struct {
	by      map[string]time.Time
	touched int
}

func (s *stamps) get(key string) (time.Time, bool) {
	s.touched++
	at, ok := s.by[key]

	return at, ok
}

func (s *stamps) put(key string, at time.Time) {
	s.touched++
	s.by[key] = at
}

func (s *stamps) drop(key string) {
	s.touched++
	delete(s.by, key)
}

// each calls visit for every key, and counts every one.
func (s *stamps) each(visit func(key string, at time.Time)) {
	for key, at := range s.by {
		s.touched++
		visit(key, at)
	}
}

func (s *stamps) len() int { return len(s.by) }
