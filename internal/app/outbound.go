package app

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
)

// outboundError is a network failure of a request this server made to
// another host, worded for an operator. It unwraps to the error Go reported.
type outboundError struct {
	sentence string
	cause    error
}

func (f *outboundError) Error() string { return f.sentence }

func (f *outboundError) Unwrap() error { return f.cause }

// reachFailure words a failed outbound request as a plain sentence naming
// the host and the cause, and answers nil for an error that is not a
// network failure. A DNS fault on this host otherwise reads as a fault of
// the remote service.
func reachFailure(err error) error {
	host := failedHost(err)

	if lookup, isLookup := errors.AsType[*net.DNSError](err); isLookup {
		if lookup.IsNotFound {
			return &outboundError{fmt.Sprintf("the name %s does not exist in DNS", host), err}
		}

		answer := ""
		if lookup.Server != "" {
			answer = fmt.Sprintf(": the DNS server %s answered %q", lookup.Server, lookup.Err)
		}

		return &outboundError{
			fmt.Sprintf(
				"cannot resolve %s (DNS failure on this host)%s. Check the host's /etc/resolv.conf",
				host,
				answer,
			),
			err,
		}
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return &outboundError{fmt.Sprintf("%s refused the connection", host), err}
	}

	if timeout, isNet := errors.AsType[net.Error](err); isNet && timeout.Timeout() {
		return &outboundError{fmt.Sprintf("%s did not answer in time", host), err}
	}

	return nil
}

// failedHost is the host the request was for: the name a lookup failed on,
// else the host of the request's address.
func failedHost(err error) string {
	if lookup, isLookup := errors.AsType[*net.DNSError](err); isLookup && lookup.Name != "" {
		return lookup.Name
	}

	if request, isRequest := errors.AsType[*url.Error](err); isRequest {
		if address, parseErr := url.Parse(request.URL); parseErr == nil && address.Host != "" {
			return address.Host
		}
	}

	return "the host"
}
