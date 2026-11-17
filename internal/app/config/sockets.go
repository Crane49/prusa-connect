package config

import "net"

// SameSocket reports whether two addresses could be answered by one listener: a
// shared port, and a host that is the same or is a wildcard on either side.
//
// It is exported because the second listener is a connector's own, and the
// composition root compares the two addresses. A deployment that gave the API
// listener and a device listener one socket is refused there, before either
// is opened.
func SameSocket(first, second string) bool {
	if first == "" || second == "" {
		return false
	}

	return sameSocket(first, second)
}

// sameSocket reports whether two addresses could be answered by one listener: a
// shared port, and a host that is the same or is a wildcard on either side.
func sameSocket(first, second string) bool {
	firstHost, firstPort, err := net.SplitHostPort(first)
	if err != nil {
		return false
	}

	secondHost, secondPort, err := net.SplitHostPort(second)
	if err != nil {
		return false
	}

	if firstPort != secondPort {
		return false
	}

	return anyHost(firstHost) || anyHost(secondHost) || firstHost == secondHost
}

// anyHost reports whether a host half binds every interface: the empty host
// of ":8080", or one of the two unspecified addresses.
func anyHost(host string) bool {
	return host == "" || host == "0.0.0.0" || host == "::"
}
