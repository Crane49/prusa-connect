package join

import (
	"context"
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// awaitingApproval is a refinement of CodeJoinUnauthorized, a more specific
// form of that code. A transport binding defines such a code for a mode where
// the node above records a new identity and waits for an operator to approve
// it.
const awaitingApproval fractal.Code = fractal.CodeJoinUnauthorized | 1

// unavailableRefined is a refinement of CodeJoinUnavailable.
const unavailableRefined fractal.Code = fractal.CodeJoinUnavailable | 2

// codeAuthorizer returns an Authorizer that answers every request with err.
func codeAuthorizer(err error) Authorizer {
	return AuthorizerFunc(func(context.Context, fractal.JoinRequest, fractal.NodePath) error {
		return err
	})
}

// TestAnAuthorizerRefinesTheCodeOfItsOwnCheck checks which code the Parent
// sends when an Authorizer returns a CodeError. The Parent sends CodeError.Code
// when it is a refinement of the code the Parent picked from the error.
// Otherwise it sends the code it picked.
func TestAnAuthorizerRefinesTheCodeOfItsOwnCheck(t *testing.T) {
	t.Parallel()
	denied := errors.New("the identity waits for an operator")
	broken := errors.New("the key source is unreachable")
	cases := []struct {
		err  error
		name string
		want fractal.Code
	}{
		{
			name: "a refinement of the unauthorized code is sent",
			err:  &CodeError{Code: awaitingApproval, Err: errors.Join(ErrUnauthorized, denied)},
			want: awaitingApproval,
		},
		{
			name: "a refinement of the unavailable code is sent",
			err:  &CodeError{Code: unavailableRefined, Err: broken},
			want: unavailableRefined,
		},
		{
			name: "the collision code from an authorizer is ignored",
			err:  &CodeError{Code: fractal.CodeJoinCollision, Err: errors.Join(ErrUnauthorized, denied)},
			want: fractal.CodeJoinUnauthorized,
		},
		{
			name: "the unavailable code on a denial is ignored",
			err:  &CodeError{Code: fractal.CodeJoinUnavailable, Err: errors.Join(ErrUnauthorized, denied)},
			want: fractal.CodeJoinUnauthorized,
		},
		{
			name: "the unauthorized code on an error that does not wrap ErrUnauthorized is ignored",
			err:  &CodeError{Code: fractal.CodeJoinUnauthorized, Err: broken},
			want: fractal.CodeJoinUnavailable,
		},
		{
			name: "a code from the reserved range is ignored",
			err:  &CodeError{Code: fractal.ReservedRangeFirst, Err: errors.Join(ErrUnauthorized, denied)},
			want: fractal.CodeJoinUnauthorized,
		},
		{
			name: "an error with no CodeError gets CodeJoinUnauthorized",
			err:  errors.Join(ErrUnauthorized, denied),
			want: fractal.CodeJoinUnauthorized,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, func(cfg *ParentConfig) { cfg.Authorizer = codeAuthorizer(c.err) })
			f.mustDeny(t, request("printer-1", "child-key"), c.want)
		})
	}
}

// TestARefinedDenialKeepsTheRuleOfItsBaseCode checks that fractal.JoinRetry
// gives a refinement the retry rule of its base code, so a node that does not
// know the refinement still retries correctly. It also checks that
// Outcome.Cause still wraps the errors the Authorizer returned.
func TestARefinedDenialKeepsTheRuleOfItsBaseCode(t *testing.T) {
	t.Parallel()
	denied := errors.New("the identity waits for an operator")
	f := newFixture(t, func(cfg *ParentConfig) {
		cfg.Authorizer = codeAuthorizer(&CodeError{
			Code: awaitingApproval,
			Err:  errors.Join(ErrUnauthorized, denied),
		})
	})
	out := f.mustDeny(t, request("printer-1", "child-key"), awaitingApproval)
	if !errors.Is(out.Cause, ErrUnauthorized) || !errors.Is(out.Cause, denied) {
		t.Errorf("Cause is %v, wanted it to wrap the errors the authorizer returned", out.Cause)
	}
	retry, known := fractal.JoinRetry(out.Denial.Code)
	if !known || retry != fractal.RetryWithNewCredential {
		t.Errorf("the retry rule of the refinement is %s, wanted %s", retry, fractal.RetryWithNewCredential)
	}
}

// TestACodeErrorReadsAsItsReason checks CodeError: its text has the code and
// the reason, and errors.Is finds the wrapped reason.
func TestACodeErrorReadsAsItsReason(t *testing.T) {
	t.Parallel()
	denied := errors.New("the identity waits for an operator")
	err := &CodeError{Code: awaitingApproval, Err: denied}
	if !errors.Is(err, denied) {
		t.Errorf("errors.Is does not find the reason in %v", err)
	}
	if got := err.Error(); got != awaitingApproval.String()+": "+denied.Error() {
		t.Errorf("the error text is %q, wanted the code and the reason", got)
	}
	if got := (&CodeError{Code: awaitingApproval}).Error(); got != awaitingApproval.String() {
		t.Errorf("the text of an error with no reason is %q, wanted the code alone", got)
	}
}
