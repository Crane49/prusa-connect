package threemf

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
)

// LabelledObject is one object a sliced file names, from its objects_info
// value. PrusaSlicer writes objects_info as a comment in text G-code and as a
// printer metadata key in binary G-code, when it labels objects.
type LabelledObject struct {
	// ID is the object's number: its position in objects_info. It is the
	// number the G-code passes to M486 S and the number a printer reports
	// when it lists the objects it can cancel.
	ID int `json:"id"`
	// Name is the label the slicer gave the object, such as "Cube" or
	// "Cube (Instance 2)". It is not unique.
	Name string `json:"name"`
	// Polygon is the object's outline on the bed it was sliced on, in that
	// bed's own coordinates.
	Polygon []Point `json:"polygon"`
}

// pointValues is how many numbers a point on the plane is written as.
const pointValues = 2

// maxLabelledObjects bounds how many objects one objects_info value may name.
// A printer cancels at most a thousand and twenty-four.
const maxLabelledObjects = 4096

// ParseObjectsInfo reads an objects_info value, such as
// {"objects":[{"name":"Cube","polygon":[[10,10],[20,10],[20,20]]}]}. A point
// that is not a finite pair of numbers is left out of its polygon.
func ParseObjectsInfo(value string) ([]LabelledObject, error) {
	var doc struct {
		Objects []struct {
			Name    string      `json:"name"`
			Polygon [][]float64 `json:"polygon"`
		} `json:"objects"`
	}

	if err := json.Unmarshal([]byte(value), &doc); err != nil {
		return nil, fmt.Errorf("%w: objects_info: %w", ErrFormat, err)
	}

	if len(doc.Objects) > maxLabelledObjects {
		return nil, limitError("objects_info", maxLabelledObjects)
	}

	out := make([]LabelledObject, len(doc.Objects))

	for i, object := range doc.Objects {
		out[i] = LabelledObject{ID: i, Name: object.Name}

		for _, point := range object.Polygon {
			if len(point) < pointValues || !finite(point[0]) || !finite(point[1]) {
				continue
			}

			out[i].Polygon = append(out[i].Polygon, Point{X: point[0], Y: point[1]})
		}
	}

	return out, nil
}

// LabelMatch ties one labelled object of a sliced file to the instance it was
// sliced from.
type LabelMatch struct {
	// Label is the LabelledObject.ID.
	Label int `json:"label"`
	// Instance is the instance in the project.
	Instance InstanceRef `json:"instance"`
	// Item is the instance's build item.
	Item int `json:"item"`
	// DistanceMM is how far apart the two outlines' centres are.
	DistanceMM float64 `json:"distance_mm"`
}

// MatchTolerance is how far apart, in millimetres, the centre of a labelled
// outline and the centre of an instance's outline may be and still match.
// The slicer simplifies the outline it writes to about 0.05 mm.
const MatchTolerance = 0.5

// MatchLabels finds the instance each labelled object of a file sliced from
// bed was sliced from.
//
// A label's name is not enough: the slicer numbers instances per bed, leaves
// the number out when an object has one instance on the bed, and two objects
// may share a name. So the match is by place. Each labelled outline is moved
// from the bed's coordinates to the project's by the bed's Offset, and paired
// with the instance on the bed whose outline has the nearest centre,
// within MatchTolerance. The closest pairs are taken first. In a tie, a pair
// whose names agree wins, and then an instance the project prints. A label
// with no instance near it is left out.
//
// MatchLabels is made for a PrusaSlicer project and a file PrusaSlicer
// sliced from it. For a project of another dialect it promises nothing.
//
// An instance the project does not print is matched too. A Selection may
// name one, and the file sliced from that Rewrite labels it.
func (p *Project) MatchLabels(bed BedRef, labels []LabelledObject) []LabelMatch {
	b := p.Bed(bed)
	if b == nil {
		return nil
	}

	candidates := p.labelCandidates(b, labels)

	slices.SortStableFunc(candidates, func(x, y labelCandidate) int {
		if c := cmpFloat(x.match.DistanceMM, y.match.DistanceMM); c != 0 {
			return c
		}

		switch {
		case x.sameName != y.sameName:
			return boolOrder(x.sameName)
		case x.printable != y.printable:
			return boolOrder(x.printable)
		}

		return 0
	})

	usedLabel, usedItem := map[int]bool{}, map[int]bool{}

	var out []LabelMatch

	for _, c := range candidates {
		if usedLabel[c.match.Label] || usedItem[c.match.Item] {
			continue
		}

		usedLabel[c.match.Label], usedItem[c.match.Item] = true, true
		out = append(out, c.match)
	}

	slices.SortFunc(out, func(x, y LabelMatch) int { return x.Label - y.Label })

	return out
}

// labelCandidate is one pairing of a label and an instance close enough to
// match.
type labelCandidate struct {
	match     LabelMatch
	sameName  bool
	printable bool
}

// boolOrder sorts true before false, for one side of a comparison already
// known to differ from the other.
func boolOrder(first bool) int {
	if first {
		return -1
	}

	return 1
}

// labelCandidates pairs every label with every instance of the bed
// whose outline centre is within MatchTolerance of the label's.
func (p *Project) labelCandidates(b *Bed, labels []LabelledObject) []labelCandidate {
	type placed struct {
		ref       InstanceRef
		item      int
		centre    Point
		name      string
		printable bool
	}

	var instances []placed

	for _, ref := range b.Instances {
		inst := p.Instance(ref)
		if inst == nil {
			continue
		}

		if at, found := centroid(inst.Outline); found {
			instances = append(
				instances,
				placed{
					ref: ref, item: inst.Item, centre: at, name: p.Objects[ref.Object].Name, printable: inst.Printable,
				},
			)
		}
	}

	var out []labelCandidate

	for _, label := range labels {
		centre, found := centroid(hull(translate(label.Polygon, b.Offset)))
		if !found {
			continue
		}

		for _, inst := range instances {
			distance := math.Hypot(inst.centre.X-centre.X, inst.centre.Y-centre.Y)
			if distance > MatchTolerance {
				continue
			}

			out = append(out, labelCandidate{
				match:     LabelMatch{Label: label.ID, Instance: inst.ref, Item: inst.item, DistanceMM: distance},
				sameName:  labelNames(inst.name, label.Name),
				printable: inst.printable,
			})
		}
	}

	return out
}

// labelNames reports whether a label could name the object: the same name, or
// the name followed by an instance number.
func labelNames(object, label string) bool {
	if object == "" {
		return false
	}

	if label == object {
		return true
	}

	return len(label) > len(object) && label[:len(object)] == object && label[len(object)] == ' '
}
