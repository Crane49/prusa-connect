package printfile

import (
	"encoding/json"
	"errors"
)

// LabelledObject is one object a sliced file names, from its objects_info
// value. PrusaSlicer writes objects_info as a comment in text G-code and as a
// printer metadata key in binary G-code, when it labels objects.
type LabelledObject struct {
	// ID is the object's number: its position in objects_info. It is the
	// number the G-code passes to M486 S and the number a printer reports
	// when it lists the objects it can cancel.
	ID int
	// Name is the label the slicer gave the object, such as "Cube" or
	// "Cube (Instance 2)". It is not unique.
	Name string
	// Polygon is the object's outline on the bed it was sliced on, in that
	// bed's own coordinates.
	Polygon []Point
}

// Point is one point of an outline on the bed, in millimetres.
type Point struct {
	X float64
	Y float64
}

// pointValues is how many numbers a point on the plane is written as.
const pointValues = 2

// maxLabelledObjects bounds how many objects one objects_info value may name.
// A printer cancels at most a thousand and twenty-four.
const maxLabelledObjects = 4096

// errTooManyObjects refuses an objects_info value naming more objects than
// maxLabelledObjects.
var errTooManyObjects = errors.New("printfile: objects_info names too many objects")

// parseObjectsInfo reads an objects_info value, such as
// {"objects":[{"name":"Cube","polygon":[[10,10],[20,10],[20,20]]}]}. A point
// that is not a finite pair of numbers is left out of its polygon.
func parseObjectsInfo(value string) ([]LabelledObject, error) {
	var doc struct {
		Objects []struct {
			Name    string      `json:"name"`
			Polygon [][]float64 `json:"polygon"`
		} `json:"objects"`
	}

	if err := json.Unmarshal([]byte(value), &doc); err != nil {
		return nil, err //nolint:wrapcheck // the one caller drops the reason.
	}

	if len(doc.Objects) > maxLabelledObjects {
		return nil, errTooManyObjects
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
