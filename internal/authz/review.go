package authz

import "context"

// ValidateAction refuses a name outside the closed set, with the code every
// surface here refuses it with. It exists for a transport that reads an action
// from a request body and has to say what was wrong with it before the graph is
// reached, so that one code answers the question wherever it is asked.
func ValidateAction(action Action) error {
	if action.mask() == 0 {
		return unknownAction.New("action is not in the vocabulary").With(paramAction(action))
	}

	return nil
}

// ObjectActions returns every action this subject may perform on this object,
// in the vocabulary's own order. It reads the row Can tests and selects the
// mask instead of testing a bit, so a name is in the answer exactly when Can
// permits that name. A review computed any other way is a second authorization
// implementation, and the one that says yes where Can says no is worse than no
// review at all.
//
// The empty answer is the whole of the refusal, and there is no row-existence
// branch above it. An object the subject holds nothing on and an object this
// server does not have both produce no row, so a caller cannot tell the two
// apart.
//
// Actions are named and never returned as the mask they are stored as. A mask
// is meaningful only against a bit assignment vocabulary.go is free to
// renumber, and an action a build does not carry occupies no bit at all, so a
// client handed a number cannot tell an unknown action from a denied one.
func (s *Store) ObjectActions(ctx context.Context, subject Subject, object Object) ([]Action, error) {
	if err := asks(subject); err != nil {
		return nil, err
	}

	switch object.typ {
	case TypeDevice, TypeResourceKind:
	case TypeUser, TypeGroup, TypeServiceAccount, TypeDeviceCredential, TypeNode, TypeSink, TypeHistory,
		TypeOrg, TypeDeviceGroup:
		return nil, unsupportedObject.New("actions are not defined on that object type").
			With(paramObject(object))
	default:
		return nil, unknownType.New("object type is not in the vocabulary").
			With(paramType(object.typ))
	}

	held, err := s.heldActions(ctx, s.db, subject, object)
	if err != nil {
		return nil, err
	}

	return ceilingFrom(ctx).clamp(held).actions(), nil
}

// actions names the bits a set carries, in the order Actions declares them.
//
// It answers nil for the empty set rather than an empty slice, so a caller that
// renders it as JSON has to decide what an empty answer looks like on the wire
// instead of inheriting one from here.
func (set actionSet) actions() []Action {
	if set == 0 {
		return nil
	}

	out := make([]Action, 0, len(Actions()))

	for _, action := range Actions() {
		if set&action.mask() != 0 {
			out = append(out, action)
		}
	}

	return out
}
