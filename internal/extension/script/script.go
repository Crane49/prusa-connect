// Package script is the script tier: an expression an operator writes in the
// configuration file, compiled once and run over every event an instance is
// given.
//
// A script maps one event to zero, one or many messages, and the host publishes
// them. It cannot loop, recurse, define a function or assign to anything,
// because the grammar has none of those. It reaches nothing this package does
// not put in its environment.
//
// Each message names an endpoint the operator declared. The operator's address
// decides what the host does with it. An HTTP address is posted to, and
// event:<event type> goes to this server's own bus, so a script can emit a new
// fact as well as reshape one. See internal/extension/bus.
//
// # Bounds the host applies
//
// The runtime's own limits do not protect a host, so this package applies its
// own.
//
//   - a source longer than MaxSourceBytes is refused before the parser sees it.
//     The parser recurses with no depth limit, so a deep enough expression ends
//     in a stack overflow that takes the process with it. No recover catches
//     it, and the node budget cannot stop it, because nodes are counted once
//     the tree already exists
//   - the repeat builtin is disabled. It multiplies a length by a count, so a
//     short script using it can allocate gigabytes before the memory budget
//     refuses the result
//   - the node budget is set here, so it is reviewed beside the source-length
//     bound
//   - the memory budget is left at the runtime's default
//
// Wall time is not bounded. The language has no loop, so the node count bounds
// what an expression can cost, and there is no way to interrupt a running
// program. The caller contains it: every script runs on the goroutine draining
// one instance's queue, so a slow script slows that instance and nothing else.
package script

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/prusa3d/prusa-connect/sdk/scriptapi"
)

const (
	// MaxSourceBytes is the longest script this host compiles. The parser uses
	// a slice of stack for every level of nesting, so the limit keeps the worst
	// case inside the stack of the smallest supported machine.
	MaxSourceBytes = 8 << 10

	// MaxNodes is the node budget. It repeats the runtime's own default here,
	// so both bounds on a script's size are read together.
	MaxNodes = 10_000
)

// The refusals this package reports. Callers inspect them with errors.Is.
var (
	// ErrInvalidSource reports a script the host will not compile.
	ErrInvalidSource = errors.New("script: the source cannot be compiled")
	// ErrInvalidResult reports a script that compiled and returned something
	// that is not a message.
	ErrInvalidResult = errors.New("script: the script returned something that is not a message")
)

// environment is everything a script can reach. The expr tags are the names an
// author writes. A name that is not a field here is a compile error naming the
// token, so this type is the whole surface a script has.
//
// The functions are everything this host adds. None of them performs an effect
// and none takes a destination. A script that could start an effect of its own
// could make host calls the host cannot count or bound.
type environment struct {
	// Event is the one fact this run is about.
	Event scriptapi.Event `expr:"event"`
	// Msg builds one message. It exists so the checker sees the field names. A
	// map literal's keys and types are not checked, so a misspelled endpoint
	// would compile and ship.
	Msg func(endpoint, key, body string) scriptapi.Message `expr:"msg"`
	// Fan collects several messages into the list the host publishes.
	Fan func(...scriptapi.Message) []scriptapi.Message `expr:"fan"`
	// ToJSON encodes a value compactly. It shadows the builtin of the same
	// name, which indents with two spaces and would send those bytes with every
	// message a script builds.
	ToJSON func(any) (string, error) `expr:"toJSON"`
}

// Program is one compiled script and the machines that run it.
//
// Compiling costs far more than one call, so it happens once at instance start.
// A vm.VM is not safe for concurrent use, so the pool gives one machine to one
// caller at a time.
type Program struct {
	program  *vm.Program
	machines sync.Pool
}

// Compile turns an operator's expression into a program, applying every bound
// before the parser is reached.
//
// The error is the runtime's own, unchanged. It names the line, the column
// and the token, and rewriting it would lose the only part an author can act
// on.
func Compile(source string) (*Program, error) {
	if len(source) > MaxSourceBytes {
		return nil, fmt.Errorf("%w: it is %d bytes and the limit is %d",
			ErrInvalidSource, len(source), MaxSourceBytes)
	}

	compiled, err := expr.Compile(source,
		expr.Env(environment{}),
		expr.DisableBuiltin("repeat"),
		expr.MaxNodes(MaxNodes),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}

	return &Program{
		program:  compiled,
		machines: sync.Pool{New: func() any { return &vm.VM{} }},
	}, nil
}

// Run evaluates the program over one event and returns what it asked the host
// to publish. A script that returns nothing drops the event.
func (p *Program) Run(fact scriptapi.Event) ([]scriptapi.Message, error) {
	machine, _ := p.machines.Get().(*vm.VM)
	defer p.machines.Put(machine)

	returned, err := machine.Run(p.program, environment{
		Event:  fact,
		Msg:    newMessage,
		Fan:    fan,
		ToJSON: toJSON,
	})
	if err != nil {
		return nil, err
	}

	return messagesOf(returned)
}

// messagesOf reads what a script returned: one message, a list built by fan, or
// an array literal. The checker types an array literal's elements one by one
// and leaves the slice itself untyped.
func messagesOf(returned any) ([]scriptapi.Message, error) {
	switch value := returned.(type) {
	case nil:
		return nil, nil
	case scriptapi.Message:
		return []scriptapi.Message{value}, nil
	case []scriptapi.Message:
		return value, nil
	case []any:
		out := make([]scriptapi.Message, 0, len(value))

		for i, element := range value {
			message, ok := element.(scriptapi.Message)
			if !ok {
				return nil, fmt.Errorf("%w: element %d is a %T, build it with msg()",
					ErrInvalidResult, i, element)
			}

			out = append(out, message)
		}

		return out, nil
	default:
		return nil, fmt.Errorf("%w: it returned a %T, build one with msg() or several with fan()",
			ErrInvalidResult, returned)
	}
}

// newMessage is the msg() constructor.
func newMessage(endpoint, key, body string) scriptapi.Message {
	return scriptapi.Message{Endpoint: endpoint, Key: key, Body: body}
}

// fan is the fan() constructor.
func fan(messages ...scriptapi.Message) []scriptapi.Message { return messages }

// toJSON is the compact encoder that shadows the builtin.
//
// The error is returned so the runtime can turn it into a script error carrying
// the source location. Division by zero yields an infinity that no JSON encoder
// writes.
func toJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("toJSON: %w", err)
	}

	return string(encoded), nil
}
