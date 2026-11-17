package bridge

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// mappingFile is the mapping table, mapping.json, built into the binary.
//
//go:embed mapping.json
var mappingFile []byte

// mappingVersion is the version of the file format ParseMapping reads.
const mappingVersion = 1

// The placeholders of mapping.json. InstanceID replaces devicePlaceholder with
// the device's name and numberPlaceholder with the number of a head or a
// slot. A state path of an interface with one instance per head or slot names
// that tool with toolPlaceholder.
const (
	devicePlaceholder = "{device}"
	numberPlaceholder = "{number}"
	toolPlaceholder   = "{tool}"
)

// The values of "per": an interface with one instance per print head, or per
// slot of the multi-material unit.
const (
	perHead = "head"
	perSlot = "slot"
)

// The errors ParseMapping returns when it refuses a mapping table. Each one is
// a mistake in mapping.json, and the tests of this package load the file, so
// they find the mistake before a release.
var (
	// ErrMappingVersion means the version of the table is not mappingVersion.
	ErrMappingVersion = errors.New("the mapping table names another format version")
	// ErrUnknownInterface means the table names an interface that no
	// definition file declares.
	ErrUnknownInterface = errors.New("the mapping table names an interface the definitions do not declare")
	// ErrUnknownProperty means the table names a property that the interface
	// does not declare.
	ErrUnknownProperty = errors.New("the mapping table names a property the interface does not declare")
	// ErrUnknownMethod means the table names a method that the interface does
	// not declare.
	ErrUnknownMethod = errors.New("the mapping table names a method the interface does not declare")
	// ErrUnknownEnum means a property or argument row names an entry of
	// "enums" that the table does not have.
	ErrUnknownEnum = errors.New("the mapping table names a value table it does not declare")
	// ErrPrinterName means a value table gives a value a printer name that the
	// same table does not convert to that value.
	ErrPrinterName = errors.New("a printer name of a value table converts to another value")
	// ErrUnknownDerivation means a property row names a "derive" reading the
	// bridge does not know.
	ErrUnknownDerivation = errors.New("the mapping table names a derived reading the bridge does not know")
	// ErrUnknownEncoding means a property row names an "encode" the bridge
	// does not know.
	ErrUnknownEncoding = errors.New("the mapping table names an encoding the bridge does not know")
	// ErrUnknownState means a method row names a printer state the
	// vocabulary does not have.
	ErrUnknownState = errors.New("the mapping table names a printer state the vocabulary does not have")
	// ErrUncovered means the table says nothing about an interface, a property
	// or a method of the definitions. Each one needs a row, or an entry in
	// "absent" that says this server does not provide it.
	ErrUncovered = errors.New("the mapping table covers neither this property nor this method")
	// ErrAbsentNotNullable means an "absent" entry names a property that may
	// not be null. The bridge reports an absent property as null, so it cannot
	// serve that interface.
	ErrAbsentNotNullable = errors.New("a property this server does not provide is declared as never null")
	// ErrBadInstance means an instance template has no "{device}"
	// placeholder, or names "{number}" exactly when "per" names no tools.
	ErrBadInstance = errors.New("the instance template does not produce an instance id")
	// ErrBadParam means an argument names a parameter position that the method
	// does not have.
	ErrBadParam = errors.New("the argument names a parameter the method does not have")
	// ErrNotWritable means a row has a "write" block for a read-only property.
	ErrNotWritable = errors.New("a read-only property carries a verb for changing it")
	// ErrNoWriteVerb means a row has no "write" block for a writable property.
	ErrNoWriteVerb = errors.New("a writable property carries no verb for changing it")
	// ErrDuplicateRow means one interface has two rows for the same property
	// or the same method.
	ErrDuplicateRow = errors.New("the mapping table names one property or method twice")
)

// Mapping is the parsed mapping table. It says which Fractal interfaces a
// device has, where each property value comes from, and which device command
// a property change or a method call becomes.
//
// Nothing writes to a Mapping after ParseMapping returns, so goroutines can
// read it without a lock.
type Mapping struct {
	interfaces []*InterfaceMap
	byName     map[fractal.InterfaceName]*InterfaceMap
	enums      map[string]*valueTable
	absent     []Absent
	paths      map[string]struct{}
}

// InterfaceMap is one interface the bridge registers for every device, with
// the source of each property and the command of each method.
type InterfaceMap struct {
	// Name is the Interface Name.
	Name fractal.InterfaceName
	// Definition is the interface as its definition file declares it.
	Definition *schema.Definition
	// Properties are the properties the bridge reads from a device, in the
	// order of the file.
	Properties []*PropertyMap
	// Methods are the methods the bridge turns into a command, in the order of
	// the file.
	Methods []*MethodMap
	// Per is "head" for an interface with one instance per print head,
	// "slot" for one per slot of the multi-material unit, and empty for one
	// instance per device.
	Per string
	// ActiveIn are the values of Printer1.state the interface has readings
	// in. In any other state every property reads null. It is empty for an
	// interface that always has its readings.
	ActiveIn []string

	instance string
	byProp   map[fractal.PropertyName]*PropertyMap
	byMethod map[fractal.MethodName]*MethodMap
	absent   map[fractal.PropertyName]struct{}
}

// PropertyMap says where the bridge reads the value of one property and how it
// converts the value. A row sets State or Field.
type PropertyMap struct {
	// Name is the property name.
	Name fractal.PropertyName
	// State is the path in the state tree of the device where the bridge reads
	// the value. It is empty when the row sets Field.
	State string
	// Field is the field of the device record where the bridge reads the
	// value, such as "model". It is empty when the row sets State.
	Field string
	// Enum names the entry of "enums" that converts the word the device
	// reports into a value of the enumeration. It is empty when the bridge
	// does not convert the value through a table.
	Enum string
	// Encode names how the bridge writes the value of Field, such as
	// uuid-base64url. It is empty when the bridge sends the field as it is.
	Encode string
	// Derive names a reading the bridge works out of the tool tree, such as
	// the material of a head. It is empty when the row sets State or Field.
	Derive string
	// ZeroAsNull reads a value of 0 as null. A heater target is 0 while the
	// heater is off, and the interface says null for that.
	ZeroAsNull bool
	// Write says which command a requested change becomes. It is nil for a
	// read-only property.
	Write *WriteMap

	property  *schema.Property
	table     *valueTable
	templated bool
}

// WriteMap says which command a requested property change becomes. A verb is
// the name of a device command, such as core.command.set_bed_temperature.
type WriteMap struct {
	// Verb is the verb of the command.
	Verb string
	// Arg is the name of the command argument that receives the new value.
	Arg string
	// NullAs is the text the bridge sends when the request asks for null. A
	// heater target uses "0", because null means the heater is off. When it is
	// empty, the bridge refuses a request for null.
	NullAs string
	// Round sends the value rounded to a whole number, for a command whose
	// argument is an integer.
	Round bool
	// ToolArg is the name of the command argument that names the head, for
	// an interface with one instance per head. The bridge sends the head's
	// number counted from 0, and only to a device that declares the argument.
	// It is empty for a command that takes no head.
	ToolArg string
}

// MethodMap says which command one method becomes and how the parameters of
// the method become the arguments of the command.
type MethodMap struct {
	// Name is the method name.
	Name fractal.MethodName
	// Verb is the verb of the command.
	Verb string
	// Args are the arguments of the command, in the order of the file.
	Args []ArgMap
	// Note tells an operator where the command does something other than the
	// interface describes. It is empty when there is no difference.
	Note string
	// OnlyIn are the values of Printer1.state the method is accepted in, and
	// RefuseIn the values it is refused in. The bridge refuses a call outside
	// OnlyIn, when OnlyIn is set, and in RefuseIn, before it sends a command
	// (domain-3d-print.md).
	OnlyIn, RefuseIn []string
	// NeedsOneOf names properties of the interface, at least one of which
	// must have a value that is not null and not one of the listed values.
	// Extrude uses it, because it needs a temperature to heat to.
	NeedsOneOf map[fractal.PropertyName][]string
	// ActiveHeadOnly refuses the method on any head but the active one,
	// because its command takes no head and acts on the active one.
	ActiveHeadOnly bool

	method *schema.Method
}

// ArgMap says where the value of one command argument comes from.
type ArgMap struct {
	// Arg is the name of the argument, as the command declares it.
	Arg string
	// Param is the position of the method parameter that gives the value,
	// counted from zero. It is NoParam when the row sets Letters.
	Param int
	// Optional means the bridge leaves the argument out when the caller sent
	// no value for the parameter. Without it, the bridge refuses the call.
	Optional bool
	// Default is the text the bridge sends when the caller sent no value,
	// for a command that needs the argument where the method does not.
	Default string
	// Round sends the value rounded to a whole number, for a command whose
	// argument is an integer.
	Round bool
	// Letters builds the text of the argument from boolean parameters: the
	// text has the letter of each parameter that is true. The home method
	// uses it to turn the parameters x, y and z into the axes "XYZ".
	Letters []LetterMap
	// URL means the parameter is a URL. The bridge passes the path of a file
	// URL as a path on the printer's own storage, and refuses a value with no
	// scheme or another scheme, unless Fractal takes it.
	URL bool
	// Refuse are the values the bridge refuses with CodeValueRefused, such as
	// NOTHING, which names no filament to load.
	Refuse []string
	// Enum names the entry of "enums" whose printerNames give the name a
	// printer knows for a value, such as FLEX for TPU. It is empty when the
	// bridge sends the value as the caller sent it.
	Enum string
	// Fractal means the parameter may be a fractal URL. The bridge then
	// fetches the file and prints it with device.print instead of sending the
	// command, when the module runs the files extension.
	Fractal bool

	printerNames map[string]string
}

// NoParam is the value of ArgMap.Param when no single parameter gives the
// value of the argument.
const NoParam = -1

// LetterMap is one letter of an argument built with ArgMap.Letters.
type LetterMap struct {
	// Param is the position of the boolean parameter, counted from zero.
	Param int
	// Letter is the text added to the argument when that parameter is true.
	Letter string
}

// Absent is one entry of "absent" in the table. It names an interface, a
// property or a method of the specification that this server does not provide,
// with the reason.
type Absent struct {
	// Interface is the Interface Name.
	Interface fractal.InterfaceName
	// Property is the property name. It is empty when the entry names a
	// method or a whole interface.
	Property fractal.PropertyName
	// Method is the method name. It is empty when the entry names a property
	// or a whole interface.
	Method fractal.MethodName
	// Reason says why this server cannot provide it.
	Reason string
}

// Whole reports whether the entry names a whole interface, and not one
// property or one method.
func (a Absent) Whole() bool { return a.Property == "" && a.Method == "" }

// valueTable is one entry of "enums". It converts a word a device reports into
// a value of an enumeration. raw and fallRaw are values and fallback encoded
// as JSON strings ahead of time, so convert allocates no memory.
type valueTable struct {
	name         string
	fallback     string
	values       map[string]string
	printerNames map[string]string
	raw          map[string]json.RawMessage
	fallRaw      json.RawMessage
	duringJob    *jobValues
}

// jobValues are the values a table gives some words while a print runs. A
// print runs while the word at path is one of running.
type jobValues struct {
	path    string
	running map[string]struct{}
	raw     map[string]json.RawMessage
}

// mappingDoc and the types after it are the JSON format of mapping.json.
// ParseMapping decodes them and builds the exported types from them.
type mappingDoc struct {
	Version    int                `json:"version"`
	Enums      map[string]enumDoc `json:"enums"`
	Interfaces []interfaceDoc     `json:"interfaces"`
	Absent     []absentDoc        `json:"absent"`
}

type enumDoc struct {
	Note      string            `json:"note"`
	Default   string            `json:"default"`
	Values    map[string]string `json:"values"`
	DuringJob *duringJobDoc     `json:"duringJob"`
	// PrinterNames are the names a printer knows for the values it names
	// otherwise, such as FLEX for TPU.
	PrinterNames map[string]string `json:"printerNames"`
}

type duringJobDoc struct {
	Note    string            `json:"note"`
	Path    string            `json:"path"`
	Running []string          `json:"running"`
	Values  map[string]string `json:"values"`
}

type interfaceDoc struct {
	Interface  string        `json:"interface"`
	Instance   string        `json:"instance"`
	Per        string        `json:"per"`
	ActiveIn   []string      `json:"activeIn"`
	Properties []propertyDoc `json:"properties"`
	Methods    []methodDoc   `json:"methods"`
}

type propertyDoc struct {
	Name       string    `json:"name"`
	State      string    `json:"state"`
	Field      string    `json:"field"`
	Enum       string    `json:"enum"`
	Derive     string    `json:"derive"`
	Encode     string    `json:"encode"`
	ZeroAsNull bool      `json:"zeroAsNull"`
	Write      *writeDoc `json:"write"`
}

type writeDoc struct {
	Verb    string `json:"verb"`
	Arg     string `json:"arg"`
	NullAs  string `json:"nullAs"`
	Round   bool   `json:"round"`
	ToolArg string `json:"toolArg"`
}

type methodDoc struct {
	Name           string              `json:"name"`
	Verb           string              `json:"verb"`
	Args           []argDoc            `json:"args"`
	Note           string              `json:"note"`
	OnlyIn         []string            `json:"onlyIn"`
	RefuseIn       []string            `json:"refuseIn"`
	NeedsOneOf     map[string][]string `json:"needsOneOf"`
	ActiveHeadOnly bool                `json:"activeHeadOnly"`
}

type argDoc struct {
	Arg      string      `json:"arg"`
	Param    *int        `json:"param"`
	Optional bool        `json:"optional"`
	Letters  []letterDoc `json:"letters"`
	URL      bool        `json:"url"`
	Fractal  bool        `json:"fractal"`
	Refuse   []string    `json:"refuse"`
	Default  string      `json:"default"`
	Round    bool        `json:"round"`
	Enum     string      `json:"enum"`
}

type letterDoc struct {
	Param  int    `json:"param"`
	Letter string `json:"letter"`
}

type absentDoc struct {
	Interface string `json:"interface"`
	Property  string `json:"property"`
	Method    string `json:"method"`
	Reason    string `json:"reason"`
}

// LoadMapping parses the mapping.json built into the binary and checks it
// against the interface definitions in registry. The table must name every
// interface, property and method of the definitions, as a row or as an
// "absent" entry.
func LoadMapping(registry *schema.Registry) (*Mapping, error) {
	return ParseMapping(registry, mappingFile)
}

// ParseMapping parses one mapping table and checks it against registry, as
// LoadMapping does. It refuses a JSON member it does not know. A test passes
// its own table.
func ParseMapping(registry *schema.Registry, data []byte) (*Mapping, error) {
	var doc mappingDoc

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("read the mapping table: %w", err)
	}

	if doc.Version != mappingVersion {
		return nil, fmt.Errorf("%w: the table says %d and this build reads %d",
			ErrMappingVersion, doc.Version, mappingVersion)
	}

	built := &Mapping{
		byName: make(map[fractal.InterfaceName]*InterfaceMap, len(doc.Interfaces)),
		enums:  make(map[string]*valueTable, len(doc.Enums)),
		paths:  make(map[string]struct{}),
	}

	for name, declared := range doc.Enums {
		for value, printerName := range declared.PrinterNames {
			if declared.Values[printerName] != value {
				return nil, fmt.Errorf("%w: %s gives %s the name %s", ErrPrinterName, name, value, printerName)
			}
		}

		built.enums[name] = newValueTable(name, declared)
	}

	for _, entry := range doc.Absent {
		built.absent = append(built.absent, Absent{
			Interface: fractal.InterfaceName(entry.Interface),
			Property:  fractal.PropertyName(entry.Property),
			Method:    fractal.MethodName(entry.Method),
			Reason:    entry.Reason,
		})
	}

	for i := range doc.Interfaces {
		one, err := built.readInterface(registry, doc.Interfaces[i])
		if err != nil {
			return nil, err
		}

		built.interfaces = append(built.interfaces, one)
		built.byName[one.Name] = one
	}

	if err := built.check(registry); err != nil {
		return nil, err
	}

	return built, nil
}

// newValueTable builds a valueTable from one entry of "enums".
func newValueTable(name string, declared enumDoc) *valueTable {
	table := &valueTable{
		name:         name,
		fallback:     declared.Default,
		values:       declared.Values,
		printerNames: declared.PrinterNames,
		raw:          make(map[string]json.RawMessage, len(declared.Values)),
	}

	for from, to := range declared.Values {
		table.raw[from] = jsonString(to)
	}

	if declared.Default != "" {
		table.fallRaw = jsonString(declared.Default)
	}

	if job := declared.DuringJob; job != nil {
		table.duringJob = &jobValues{
			path:    job.Path,
			running: make(map[string]struct{}, len(job.Running)),
			raw:     make(map[string]json.RawMessage, len(job.Values)),
		}

		for _, word := range job.Running {
			table.duringJob.running[word] = struct{}{}
		}

		for from, to := range job.Values {
			table.duringJob.raw[from] = jsonString(to)
		}
	}

	return table
}

// readInterface builds an InterfaceMap from one interface of the file. It
// refuses an interface the registry does not have, a template with no
// "{device}", and a property or a method that has two rows.
func (m *Mapping) readInterface(registry *schema.Registry, doc interfaceDoc) (*InterfaceMap, error) {
	name := fractal.InterfaceName(doc.Interface)

	definition, ok := registry.Definition(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownInterface, name)
	}

	if err := checkInstance(name, doc); err != nil {
		return nil, err
	}

	if err := checkStates(registry, string(name), doc.ActiveIn); err != nil {
		return nil, err
	}

	one := &InterfaceMap{
		Name:       name,
		Per:        doc.Per,
		ActiveIn:   doc.ActiveIn,
		Definition: definition,
		instance:   doc.Instance,
		byProp:     make(map[fractal.PropertyName]*PropertyMap, len(doc.Properties)),
		byMethod:   make(map[fractal.MethodName]*MethodMap, len(doc.Methods)),
		absent:     make(map[fractal.PropertyName]struct{}),
	}

	for _, declared := range doc.Properties {
		mapped, err := m.readProperty(definition, declared)
		if err != nil {
			return nil, err
		}

		if _, seen := one.byProp[mapped.Name]; seen {
			return nil, fmt.Errorf("%w: %s.%s", ErrDuplicateRow, name, mapped.Name)
		}

		one.Properties = append(one.Properties, mapped)
		one.byProp[mapped.Name] = mapped

		if mapped.State != "" && !mapped.templated {
			m.paths[mapped.State] = struct{}{}
		}
	}

	for i := range doc.Methods {
		mapped, err := m.readMethod(registry, definition, doc.Methods[i])
		if err != nil {
			return nil, err
		}

		if _, seen := one.byMethod[mapped.Name]; seen {
			return nil, fmt.Errorf("%w: %s.%s", ErrDuplicateRow, name, mapped.Name)
		}

		one.Methods = append(one.Methods, mapped)
		one.byMethod[mapped.Name] = mapped
	}

	return one, nil
}

// readProperty builds a PropertyMap from one property row. It checks that the
// interface declares the property, that the "enums" entry exists, and that a
// row has a "write" block exactly when the property is writable.
func (m *Mapping) readProperty(
	definition *schema.Definition, doc propertyDoc,
) (*PropertyMap, error) {
	name := fractal.PropertyName(doc.Name)

	property, ok := definition.Property(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, definition.Name(), name)
	}

	mapped := &PropertyMap{
		Name:       name,
		State:      doc.State,
		Field:      doc.Field,
		Enum:       doc.Enum,
		Derive:     doc.Derive,
		Encode:     doc.Encode,
		ZeroAsNull: doc.ZeroAsNull,
		property:   property,
		templated:  strings.Contains(doc.State, toolPlaceholder),
	}

	if _, known := derivations[doc.Derive]; doc.Derive != "" && !known {
		return nil, fmt.Errorf("%w: %s.%s names %s", ErrUnknownDerivation, definition.Name(), name, doc.Derive)
	}

	if doc.Encode != "" && doc.Encode != encodeUUID {
		return nil, fmt.Errorf("%w: %s.%s names %s", ErrUnknownEncoding, definition.Name(), name, doc.Encode)
	}

	if doc.Enum != "" {
		table, known := m.enums[doc.Enum]
		if !known {
			return nil, fmt.Errorf("%w: %s.%s names %s",
				ErrUnknownEnum, definition.Name(), name, doc.Enum)
		}

		mapped.table = table

		if table.duringJob != nil {
			m.paths[table.duringJob.path] = struct{}{}
		}
	}

	if doc.Write != nil {
		if !property.Writable() {
			return nil, fmt.Errorf("%w: %s.%s", ErrNotWritable, definition.Name(), name)
		}

		mapped.Write = &WriteMap{
			Verb: doc.Write.Verb, Arg: doc.Write.Arg, NullAs: doc.Write.NullAs, Round: doc.Write.Round,
			ToolArg: doc.Write.ToolArg,
		}
	}

	if property.Writable() && mapped.Write == nil {
		return nil, fmt.Errorf("%w: %s.%s", ErrNoWriteVerb, definition.Name(), name)
	}

	return mapped, nil
}

// checkInstance refuses an instance template with no "{device}", and one
// that names "{number}" when "per" names no tools or the other way round.
func checkInstance(name fractal.InterfaceName, doc interfaceDoc) error {
	if !strings.Contains(doc.Instance, devicePlaceholder) {
		return fmt.Errorf("%w: %s carries %q, which names no device", ErrBadInstance, name, doc.Instance)
	}

	known := doc.Per == "" || doc.Per == perHead || doc.Per == perSlot
	if !known || strings.Contains(doc.Instance, numberPlaceholder) != (doc.Per != "") {
		return fmt.Errorf("%w: %s carries %q for one instance per %q", ErrBadInstance, name, doc.Instance, doc.Per)
	}

	return nil
}

// checkStates refuses a state that is not a value of PrinterState. where
// names the row in the error.
func checkStates(registry *schema.Registry, where string, states []string) error {
	enum, _ := registry.Enum(printerStateEnum)

	for _, state := range states {
		if enum == nil || !enum.Has(state) {
			return fmt.Errorf("%w: %s names %s", ErrUnknownState, where, state)
		}
	}

	return nil
}

// readNeeds builds MethodMap.NeedsOneOf and refuses a property the interface
// does not declare. where names the row in the error.
func readNeeds(
	definition *schema.Definition, where string, doc map[string][]string,
) (map[fractal.PropertyName][]string, error) {
	needs := make(map[fractal.PropertyName][]string, len(doc))

	for property, excluded := range doc {
		if _, declared := definition.Property(fractal.PropertyName(property)); !declared {
			return nil, fmt.Errorf("%w: %s needs %s", ErrUnknownProperty, where, property)
		}

		needs[fractal.PropertyName(property)] = excluded
	}

	return needs, nil
}

// readMethod builds a MethodMap from one method row. It checks that the
// interface declares the method and each property NeedsOneOf names, that every
// parameter position exists, and that every state is a value of PrinterState.
func (m *Mapping) readMethod(
	registry *schema.Registry, definition *schema.Definition, doc methodDoc,
) (*MethodMap, error) {
	name := fractal.MethodName(doc.Name)

	method, ok := definition.Method(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s", ErrUnknownMethod, definition.Name(), name)
	}

	mapped := &MethodMap{
		Name: name, Verb: doc.Verb, Note: doc.Note, OnlyIn: doc.OnlyIn, RefuseIn: doc.RefuseIn,
		ActiveHeadOnly: doc.ActiveHeadOnly, method: method,
	}

	where := string(definition.Name()) + "." + string(name)
	if err := checkStates(registry, where, slices.Concat(doc.OnlyIn, doc.RefuseIn)); err != nil {
		return nil, err
	}

	needs, err := readNeeds(definition, where, doc.NeedsOneOf)
	if err != nil {
		return nil, err
	}

	mapped.NeedsOneOf = needs

	for _, declared := range doc.Args {
		var arg ArgMap
		if arg, err = m.readArg(definition, method, name, declared); err != nil {
			return nil, err
		}

		mapped.Args = append(mapped.Args, arg)
	}

	return mapped, nil
}

// readArg builds an ArgMap from one argument of a method row. It checks that
// every parameter position exists and that the value table it names exists.
func (m *Mapping) readArg(
	definition *schema.Definition, method *schema.Method, name fractal.MethodName, declared argDoc,
) (ArgMap, error) {
	arg := ArgMap{Arg: declared.Arg, Param: NoParam, Optional: declared.Optional, URL: declared.URL,
		Fractal: declared.Fractal, Refuse: declared.Refuse, Default: declared.Default, Round: declared.Round,
		Enum: declared.Enum,
	}

	if declared.Enum != "" {
		table, known := m.enums[declared.Enum]
		if !known {
			return ArgMap{}, fmt.Errorf("%w: %s.%s argument %s names %s",
				ErrUnknownEnum, definition.Name(), name, declared.Arg, declared.Enum)
		}

		arg.printerNames = table.printerNames
	}

	if declared.Param != nil {
		if *declared.Param < 0 || *declared.Param >= method.NumParams() {
			return ArgMap{}, fmt.Errorf("%w: %s.%s argument %s names parameter %d of %d",
				ErrBadParam, definition.Name(), name, declared.Arg, *declared.Param, method.NumParams())
		}

		arg.Param = *declared.Param
	}

	for _, letter := range declared.Letters {
		if letter.Param < 0 || letter.Param >= method.NumParams() {
			return ArgMap{}, fmt.Errorf("%w: %s.%s argument %s names parameter %d of %d",
				ErrBadParam, definition.Name(), name, declared.Arg, letter.Param, method.NumParams())
		}

		arg.Letters = append(arg.Letters, LetterMap(letter))
	}

	return arg, nil
}

// check compares the table with the definitions. Every interface, property and
// method of the definitions must have a row or an "absent" entry.
func (m *Mapping) check(registry *schema.Registry) error {
	covered := make(map[string]struct{})
	whole := make(map[fractal.InterfaceName]struct{})

	for _, entry := range m.absent {
		if _, ok := registry.Definition(entry.Interface); !ok {
			return fmt.Errorf("%w: %s", ErrUnknownInterface, entry.Interface)
		}

		if entry.Whole() {
			whole[entry.Interface] = struct{}{}

			continue
		}

		covered[rowKey(entry.Interface, string(entry.Property), string(entry.Method))] = struct{}{}

		if one, ok := m.byName[entry.Interface]; ok && entry.Property != "" {
			one.absent[entry.Property] = struct{}{}
		}
	}

	for definition := range registry.Definitions() {
		if _, exposed := m.byName[definition.Name()]; !exposed {
			if _, ok := whole[definition.Name()]; !ok {
				return fmt.Errorf("%w: %s is neither exposed nor named as absent",
					ErrUncovered, definition.Name())
			}

			continue
		}

		if err := m.checkOne(definition, covered); err != nil {
			return err
		}
	}

	return nil
}

// checkOne compares one interface of the table with its definition. covered is
// the set of "absent" entries for single properties and methods.
func (m *Mapping) checkOne(definition *schema.Definition, covered map[string]struct{}) error {
	one := m.byName[definition.Name()]

	for property := range definition.Properties() {
		if _, mapped := one.byProp[property.Name()]; mapped {
			continue
		}

		key := rowKey(definition.Name(), string(property.Name()), "")
		if _, named := covered[key]; !named {
			return fmt.Errorf("%w: %s.%s", ErrUncovered, definition.Name(), property.Name())
		}

		if !property.Nullable() {
			return fmt.Errorf("%w: %s.%s", ErrAbsentNotNullable, definition.Name(), property.Name())
		}
	}

	for method := range definition.Methods() {
		if _, mapped := one.byMethod[method.Name()]; mapped {
			continue
		}

		key := rowKey(definition.Name(), "", string(method.Name()))
		if _, named := covered[key]; !named {
			return fmt.Errorf("%w: %s.%s", ErrUncovered, definition.Name(), method.Name())
		}
	}

	return nil
}

// rowKey returns the key of one property or one method in the covered set.
func rowKey(name fractal.InterfaceName, property, method string) string {
	return string(name) + "\x00" + property + "\x00" + method
}

// Interfaces returns the interfaces of the table, in the order of the file.
// The caller owns the returned slice.
func (m *Mapping) Interfaces() []*InterfaceMap {
	return append([]*InterfaceMap(nil), m.interfaces...)
}

// Interface returns one interface of the table by its name.
func (m *Mapping) Interface(name fractal.InterfaceName) (one *InterfaceMap, ok bool) {
	one, ok = m.byName[name]

	return one, ok
}

// Absent returns the "absent" entries of the table, in the order of the file.
// The caller owns the returned slice.
func (m *Mapping) Absent() []Absent {
	return append([]Absent(nil), m.absent...)
}

// WatchesPath reports whether a property of the table reads this path of the
// state tree, the path itself or the path its value table checks for a
// running print. A reading at any other path changes no property. Every member
// of a tool is watched, because a derived reading may read any of them.
func (m *Mapping) WatchesPath(path string) bool {
	_, ok := m.paths[path]

	return ok || isToolPath(path)
}

// InstanceID returns the Interface Instance ID of this interface for one
// device: the instance template with the device's own name for the network in
// it, and the number of the head or the slot, counted from 1, for an
// interface with one instance per head or slot. InstanceName says how the
// device's name is worked out.
func (i *InterfaceMap) InstanceID(device string, number int) fractal.InstanceID {
	name, _ := InstanceName(device)
	id := strings.ReplaceAll(i.instance, devicePlaceholder, name)

	return fractal.InstanceID(strings.ReplaceAll(id, numberPlaceholder, strconv.Itoa(number)))
}

// InstanceName is the device's name on the Fractal network, worked out from
// the identifier this server gave it.
//
// An Interface Instance ID follows the Identifier grammar of fractal.md, which
// is letters, digits and underscores. A device here is identified by a UUID,
// and a UUID has hyphens, so the hyphens are removed: the 32 hexadecimal
// characters that are left are inside the length the grammar allows. An
// identifier that already follows the grammar is returned unchanged.
//
// It returns false for an identifier that is not a name on the network even
// without its hyphens. The bridge then shows that device to nobody.
//
// The same identifier always gives the same name, and the bridge refuses a
// second device whose identifier gives a name a first device already took.
func InstanceName(device string) (string, bool) {
	if fractal.InstanceID(device).Validate() == nil {
		return device, true
	}

	stripped := strings.ReplaceAll(device, "-", "")
	if fractal.InstanceID(stripped).Validate() != nil {
		return "", false
	}

	return stripped, true
}

// Property returns the row of one property by its name.
func (i *InterfaceMap) Property(name fractal.PropertyName) (p *PropertyMap, ok bool) {
	p, ok = i.byProp[name]

	return p, ok
}

// Method returns the row of one method by its name.
func (i *InterfaceMap) Method(name fractal.MethodName) (m *MethodMap, ok bool) {
	m, ok = i.byMethod[name]

	return m, ok
}

// AbsentProperties returns the properties of this interface that have an
// "absent" entry. The bridge reports each of them as null.
func (i *InterfaceMap) AbsentProperties() []fractal.PropertyName {
	out := make([]fractal.PropertyName, 0, len(i.absent))
	for name := range i.absent {
		out = append(out, name)
	}

	return out
}

// jsonString encodes one text as a JSON string.
func jsonString(s string) json.RawMessage {
	encoded, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(`""`)
	}

	return encoded
}
