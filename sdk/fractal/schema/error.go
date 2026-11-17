package schema

import (
	"errors"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// The reasons a message is invalid. Every error the Validate methods, the Read
// functions and ValueType.Check return is one of these or wraps one. Test
// with errors.Is.
var (
	// ErrUnknownInterface reports that a message names an interface the
	// registry does not have.
	ErrUnknownInterface = errors.New("interface is not in the registry")
	// ErrUnknownProperty reports that a message names a property the interface
	// does not define.
	ErrUnknownProperty = errors.New("property is not in the interface")
	// ErrUnknownMethod reports that an RPC names a method the interface does
	// not define.
	ErrUnknownMethod = errors.New("method is not in the interface")
	// ErrReadOnly reports that a DES asks to change a read-only property.
	ErrReadOnly = errors.New("property is read-only")
	// ErrNotNullable reports that a value is null and the definition does not
	// allow null there.
	ErrNotNullable = errors.New("value is null and the definition forbids it")
	// ErrWrongType reports that a value is valid JSON of another type than the
	// definition declares.
	ErrWrongType = errors.New("value does not have the declared type")
	// ErrIntRange reports that an integer does not fit in a signed 64-bit
	// integer.
	ErrIntRange = errors.New("integer is outside the 64-bit signed range")
	// ErrFloatRange reports that a number is too large for a float64.
	ErrFloatRange = errors.New("number is too large for a 64-bit float")
	// ErrUnknownEnumValue reports that a string is not one of the values the
	// enumeration lists.
	ErrUnknownEnumValue = errors.New("value is not in the enumeration")
	// ErrMalformed reports that the bytes of a value are not one complete JSON
	// value.
	ErrMalformed = errors.New("value is not one JSON value")
	// ErrParamCount reports that an RPC has fewer parameters than the method
	// requires, or more than the method declares.
	ErrParamCount = errors.New("wrong number of parameters")
	// ErrUnexpectedResult reports that a RES has a result and the method
	// returns nothing.
	ErrUnexpectedResult = errors.New("method returns nothing and a result is present")
	// ErrMissingResult reports that a RES has no result and the method declares
	// a return type.
	ErrMissingResult = errors.New("method returns a value and the result is absent")
)

// PropertyError says which property of which interface is invalid and why.
// ValidateDES and ValidateREP return it. A node must reject the whole DES when
// ValidateDES returns one.
type PropertyError struct {
	// Interface is the interface the message named.
	Interface fractal.InterfaceName
	// Property is the invalid property. It is empty when the registry does not
	// have the interface.
	Property fractal.PropertyName
	// Err is the reason the property is invalid.
	Err error
}

// Error returns the interface, the property and the reason.
func (e *PropertyError) Error() string {
	if e.Property == "" {
		return string(e.Interface) + ": " + e.Err.Error()
	}
	return string(e.Interface) + "." + string(e.Property) + ": " + e.Err.Error()
}

// Unwrap returns the reason the property is invalid.
func (e *PropertyError) Unwrap() error { return e.Err }

// NoParamIndex is the Index of a ParamError that is not about one parameter:
// the interface or the method is unknown, or the number of parameters is wrong.
const NoParamIndex = -1

// ParamError says which parameter of which method is invalid and why.
// ValidateParams and ValidateRPC return it. The node answers the RPC with
// fractal.CodeInvalidParams.
type ParamError struct {
	// Interface is the interface the RPC named.
	Interface fractal.InterfaceName
	// Method is the method the RPC named.
	Method fractal.MethodName
	// Param is the name of the invalid parameter. It is empty when Index is
	// NoParamIndex.
	Param fractal.Identifier
	// Index is the position of the invalid parameter, counting from zero, or
	// NoParamIndex.
	Index int
	// Err is the reason.
	Err error
}

// Error returns the interface, the method, the parameter and the reason.
func (e *ParamError) Error() string {
	where := string(e.Interface) + "." + string(e.Method)
	if e.Index != NoParamIndex {
		where += " param " + strconv.Itoa(e.Index) + " " + string(e.Param)
	}
	return where + ": " + e.Err.Error()
}

// Unwrap returns the reason the parameter is invalid.
func (e *ParamError) Unwrap() error { return e.Err }

// Code returns the error code for the RES that answers the RPC. It is always
// fractal.CodeInvalidParams, also for an unknown method or interface.
func (e *ParamError) Code() fractal.Code { return fractal.CodeInvalidParams }

// ResultError says which method has a result that does not match its
// definition. ValidateResult and ValidateRES return it.
type ResultError struct {
	// Interface is the interface of the method.
	Interface fractal.InterfaceName
	// Method is the method that returned the result.
	Method fractal.MethodName
	// Err is the reason.
	Err error
}

// Error returns the interface, the method and the reason.
func (e *ResultError) Error() string {
	return string(e.Interface) + "." + string(e.Method) + " result: " + e.Err.Error()
}

// Unwrap returns the reason the result is invalid.
func (e *ResultError) Unwrap() error { return e.Err }
