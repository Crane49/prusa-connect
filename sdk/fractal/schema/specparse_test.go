package schema_test

import (
	"strings"
)

// The code below parses the markdown tables of the vocabulary documents,
// domain-common.md and domain-3d-print.md, into interfaces, properties, methods
// and enumerations, so that TestPrusaFilesMatchTheSpecification can compare
// them with the definition files. It parses only the table layouts those
// documents use.

// specProp is one row of a property table.
type specProp struct {
	Name     string
	Type     string
	Access   string
	Nullable bool
}

// specParam is one parameter of a method row.
type specParam struct {
	Name     string
	Type     string
	Optional bool
}

// specMethod is one row of a method table.
type specMethod struct {
	Name    string
	Params  []specParam
	Returns string
}

// specInterface is one interface section of the document.
type specInterface struct {
	Name     string
	Exposure string
	Props    []specProp
	Methods  []specMethod
}

// specDoc is the result of parseVocabulary: the interfaces and the
// enumerations of the vocabulary. EnumOrder has the enumeration names in the
// order of the documents.
type specDoc struct {
	Interfaces []specInterface
	Enums      map[string][]string
	EnumOrder  []string
}

// tableKind says which kind of table the current line belongs to.
type tableKind uint8

const (
	tableNone tableKind = iota
	tableProps
	tableMethods
	tableEnum
)

// specReader is the state of the parser while it reads the document line by
// line: the result so far, the current table, and the current enumeration.
type specReader struct {
	doc      specDoc
	table    tableKind
	inEnums  bool
	enumName string
}

// parseVocabulary reads the exposure lines and the property, method and
// enumeration tables of the vocabulary documents, given as one text.
func parseVocabulary(text string) specDoc {
	r := &specReader{doc: specDoc{Enums: map[string][]string{}}}
	for line := range strings.SplitSeq(text, "\n") {
		r.line(strings.TrimSpace(line))
	}
	return r.doc
}

// line handles one line of the document: a heading that starts an interface or
// the enumerations, the exposure line of an interface, a table row, or any
// other line, which ends the current table.
func (r *specReader) line(line string) {
	switch {
	case strings.HasPrefix(line, "## Interface: "):
		r.table = tableNone
		r.inEnums = false
		r.doc.Interfaces = append(r.doc.Interfaces, specInterface{Name: unquote(line[len("## Interface: "):])})
	case line == "## Enumerations":
		r.table = tableNone
		r.inEnums = true
	case strings.HasPrefix(line, "## "):
		r.table = tableNone
		r.inEnums = false
	case strings.HasPrefix(line, "Exposure: ") && len(r.doc.Interfaces) > 0:
		r.table = tableNone
		r.doc.Interfaces[len(r.doc.Interfaces)-1].Exposure = unquote(line[len("Exposure: "):])
	case r.inEnums && strings.HasPrefix(line, "### "):
		r.table = tableNone
		r.enumName = unquote(line[len("### "):])
	case strings.HasPrefix(line, "|"):
		r.row(splitRow(line))
	default:
		r.table = tableNone
	}
}

// row handles one table row. The first cell of a header row says which kind of
// table starts. A separator row is skipped. Any other row is data.
func (r *specReader) row(cells []string) {
	if len(cells) == 0 {
		return
	}
	if strings.HasPrefix(cells[0], "---") {
		return
	}
	switch cells[0] {
	case "Property":
		r.table = tableProps
		return
	case "Method":
		r.table = tableMethods
		return
	case "Value":
		r.table = tableEnum
		return
	case "Quantity":
		r.table = tableNone
		return
	}
	r.data(cells)
}

// data adds one data row of the current table to the result.
func (r *specReader) data(cells []string) {
	const (
		propCells   = 4
		methodCells = 3
	)
	switch r.table {
	case tableProps:
		if len(cells) < propCells || len(r.doc.Interfaces) == 0 {
			return
		}
		iface := &r.doc.Interfaces[len(r.doc.Interfaces)-1]
		iface.Props = append(iface.Props, specProp{
			Name:     unquote(cells[0]),
			Type:     unquote(cells[1]),
			Access:   strings.ToLower(cells[2]),
			Nullable: cells[3] == "yes",
		})
	case tableMethods:
		if len(cells) < methodCells || len(r.doc.Interfaces) == 0 {
			return
		}
		iface := &r.doc.Interfaces[len(r.doc.Interfaces)-1]
		iface.Methods = append(iface.Methods, specMethod{
			Name:    unquote(cells[0]),
			Params:  parseParams(cells[1]),
			Returns: parseReturns(cells[2]),
		})
	case tableEnum:
		if r.enumName == "" {
			return
		}
		if _, ok := r.doc.Enums[r.enumName]; !ok {
			r.doc.EnumOrder = append(r.doc.EnumOrder, r.enumName)
		}
		r.doc.Enums[r.enumName] = append(r.doc.Enums[r.enumName], unquote(cells[0]))
	case tableNone:
	}
}

// splitRow splits a markdown table row into its cells.
func splitRow(line string) []string {
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// unquote removes the backticks the document puts around a name.
func unquote(s string) string {
	return strings.Trim(strings.TrimSpace(s), "`")
}

// noValue is the text of a cell that means "no parameters" or "no return value"
// in the vocabulary: one em dash. It is written as a code point here, because
// make style-check refuses an em dash in a source file.
var noValue = string(rune(0x2014))

// parseReturns reads the Returns cell of a method row.
func parseReturns(cell string) string {
	if cell == "" || cell == noValue {
		return ""
	}
	return unquote(cell)
}

// parseParams parses the Parameters cell of a method row. Commas separate the
// parameters. A note in round brackets may have a comma too, so parseParams
// splits only at commas outside brackets.
func parseParams(cell string) []specParam {
	if cell == "" || cell == noValue {
		return nil
	}
	var out []specParam
	for _, piece := range splitOutsideBrackets(cell) {
		p, ok := parseParam(piece)
		if ok {
			out = append(out, p)
		}
	}
	return out
}

// parseParam parses one parameter, written as `name`: type (note). The
// parameter is optional when the note has the word "optional".
func parseParam(piece string) (specParam, bool) {
	piece = strings.TrimSpace(piece)
	colon := strings.Index(piece, ":")
	if colon < 0 {
		return specParam{}, false
	}
	name := unquote(piece[:colon])
	rest := strings.TrimSpace(piece[colon+1:])
	end := strings.IndexAny(rest, " (")
	note := ""
	if end >= 0 {
		note = rest[end:]
		rest = rest[:end]
	}
	return specParam{
		Name:     name,
		Type:     unquote(rest),
		Optional: strings.Contains(note, "optional"),
	}, true
}

// splitOutsideBrackets splits a string at the commas that are not inside round
// brackets.
func splitOutsideBrackets(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := range len(s) {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
