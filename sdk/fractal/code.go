package fractal

import (
	"errors"
	"fmt"
	"strconv"
)

// Code is the error code of a failed RPC and of a denied join request.
//
// Bit 15 is set in a code an application or a transport binding defines. Bit
// 14 is set when the caller caused the failure. Bit 13 is set when the callee
// decided to refuse. Bits 12 to 9 are reserved and must be zero. Bits 8 to 4
// number the code. Bits 3 to 0 are a refinement, and zero is the base form.
type Code uint16

// The codes the protocol defines. Each is the base form.
const (
	// CodeInternal says the method failed unexpectedly.
	CodeInternal Code = 0x0010
	// CodeJoinUnavailable says the node that received the join request could
	// not process it, for example because it has no Node Path yet.
	CodeJoinUnavailable Code = 0x0020
	// CodeDenied says the callee deliberately refused the request.
	CodeDenied Code = 0x2010
	// CodeExpired says the request arrived after its expires timestamp.
	CodeExpired Code = 0x2020
	// CodeJoinUnauthorized says the join credential was missing, invalid,
	// expired, or did not match the request.
	CodeJoinUnauthorized Code = 0x2030
	// CodeJoinCollision says the requested Node ID and the authentication
	// identity in the request conflict with a node that already joined the
	// same node: one of the two matches that node and the other does not.
	CodeJoinCollision Code = 0x2040
	// CodeJoinRevoked says the admission of the node that asks to join has
	// been revoked.
	CodeJoinRevoked Code = 0x2050
	// CodeInvalidParams says the parameters failed validation.
	CodeInvalidParams Code = 0x4010
	// CodeNodeNotFound says the recipient node could not be found.
	CodeNodeNotFound Code = 0x4020
	// CodeJoinRejected says the join request is unusable. It was malformed, it
	// named an unsupported protocol version, the node did not prove that it
	// owns its authentication identity, it requested an invalid Node ID, or
	// its Node Path would be over MaxNodePathLength.
	CodeJoinRejected Code = 0x4030
	// CodeObjectNotFound says the recipient received the request, and the
	// interface instance or the method it names does not exist there.
	CodeObjectNotFound Code = 0x4040
)

// CodeNotFound is an alias of CodeNodeNotFound.
//
// Deprecated: use CodeNodeNotFound.
const CodeNotFound = CodeNodeNotFound

// The bit layout of a Code.
const (
	codeApplicationBit = 0x8000
	codeCallerBit      = 0x4000
	codeIntentionalBit = 0x2000
	codeReservedBits   = 0x1E00
	codeRefinementBits = 0x000F

	// ReservedRangeFirst is the first code of the range the protocol reserves.
	ReservedRangeFirst Code = 0x6000
	// ReservedRangeLast is the last code of the range the protocol reserves.
	ReservedRangeLast Code = 0x61FF
)

// ErrReservedCode reports that a code is in the reserved range, or that a
// protocol-defined code has a reserved bit set. A node must not send such a
// code.
var ErrReservedCode = errors.New("error code is reserved")

// codeRow pairs one defined code with its constant name in the protocol.
type codeRow struct {
	code Code
	name string
}

// definedCodes lists the codes in the order of the Fractal specification.
var definedCodes = [...]codeRow{
	{CodeInternal, "ERR_INTERNAL"},
	{CodeJoinUnavailable, "ERR_JOIN_UNAVAILABLE"},
	{CodeDenied, "ERR_DENIED"},
	{CodeExpired, "ERR_EXPIRED"},
	{CodeJoinUnauthorized, "ERR_JOIN_UNAUTHORIZED"},
	{CodeJoinCollision, "ERR_JOIN_COLLISION"},
	{CodeJoinRevoked, "ERR_JOIN_REVOKED"},
	{CodeInvalidParams, "ERR_INVALID_PARAMS"},
	{CodeNodeNotFound, "ERR_NODE_NOT_FOUND"},
	{CodeJoinRejected, "ERR_JOIN_REJECTED"},
	{CodeObjectNotFound, "ERR_OBJECT_NOT_FOUND"},
}

// DefinedCodes returns every code the protocol defines, in the order of the
// Fractal specification. The caller owns the returned slice.
func DefinedCodes() []Code {
	out := make([]Code, 0, len(definedCodes))
	for _, row := range definedCodes {
		out = append(out, row.code)
	}
	return out
}

// ApplicationDefined reports whether bit 15 is set. An application or a
// transport binding sets it on every code it defines.
func (c Code) ApplicationDefined() bool { return c&codeApplicationBit != 0 }

// ProtocolDefined reports whether bit 15 is clear. It is clear in every code
// the protocol defines.
func (c Code) ProtocolDefined() bool { return !c.ApplicationDefined() }

// CallerSide reports whether the caller caused the failure. Bit 14 is then set.
func (c Code) CallerSide() bool { return c&codeCallerBit != 0 }

// CalleeSide reports whether the callee caused the failure. Bit 14 is then
// clear.
func (c Code) CalleeSide() bool { return !c.CallerSide() }

// Intentional reports whether the callee decided to refuse the request. Bit 13
// is then set.
func (c Code) Intentional() bool { return c&codeIntentionalBit != 0 }

// Base returns the code with bits 3 to 0, the refinement, set to zero.
func (c Code) Base() Code { return c &^ codeRefinementBits }

// Refinement returns bits 3 to 0. Zero means the code is a base form.
func (c Code) Refinement() uint8 { return uint8(c & codeRefinementBits) }

// ReservedBitsZero reports whether bits 12 to 9 are zero. Every
// protocol-defined code must have them at zero.
func (c Code) ReservedBitsZero() bool { return c&codeReservedBits == 0 }

// InReservedRange reports whether the code is between ReservedRangeFirst and
// ReservedRangeLast. A node must not send a code in that range.
func (c Code) InReservedRange() bool {
	return c >= ReservedRangeFirst && c <= ReservedRangeLast
}

// Valid reports whether a node may send the code. A protocol-defined code must
// have bits 12 to 9 at zero and must be outside the reserved range. An
// application-defined code may have any value in bits 14 to 0.
func (c Code) Valid() bool {
	if c.InReservedRange() {
		return false
	}
	return c.ApplicationDefined() || c.ReservedBitsZero()
}

// Is reports whether the code equals base or is a refinement of base. It
// compares the two codes with the refinement bits of each set to zero.
func (c Code) Is(base Code) bool { return c.Base() == base.Base() }

// Refine returns the code with its refinement set to n. It returns an error
// when n is over 15 and when the code already has a refinement.
func (c Code) Refine(n uint8) (Code, error) {
	if n > codeRefinementBits {
		return 0, errors.New("refinement over 15")
	}
	if c.Refinement() != 0 {
		return 0, errors.New("code already has a refinement")
	}
	return c | Code(n), nil
}

// Name returns the constant name the protocol gives the code, for example
// "ERR_DENIED". ok is false for a code the protocol does not define, and for
// a refinement of a code it defines.
func (c Code) Name() (name string, ok bool) {
	for _, row := range definedCodes {
		if row.code == c {
			return row.name, true
		}
	}
	return "", false
}

// String returns the constant name of a defined code, "ERR_DENIED+1" for a
// refinement of one, and the value in hexadecimal for any other code.
func (c Code) String() string {
	if name, ok := c.Name(); ok {
		return name
	}
	if name, ok := c.Base().Name(); ok {
		return name + "+" + strconv.Itoa(int(c.Refinement()))
	}
	return fmt.Sprintf("0x%04X", uint16(c))
}
