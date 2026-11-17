// Package document is the shape the project routes answer in, and the
// rendering of a read project into it (ADR-20261027-4tq).
//
// The words are the API's own and do not depend on the slicer that wrote the
// file. An instance's outline and box are in the coordinates of its bed, so a
// page that draws one bed draws it at the origin.
package document
