package fractal

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Props is the props field of a REP and of a DES: property values by property
// name. This package does not decode a value.
type Props map[PropertyName]json.RawMessage

// validate applies the Property Name grammar to every key.
func (p Props) validate() error {
	for name := range p {
		if err := name.Validate(); err != nil {
			return fmt.Errorf("props: %w", err)
		}
	}
	return nil
}

// REP reports the state of one interface instance of the sender. It travels
// upward. Every node between the sender and the root processes it and forwards
// it unchanged.
type REP struct {
	Version   uint          `json:"version"`
	Sender    NodePath      `json:"sender"`
	Interface InterfaceName `json:"interface"`
	Instance  InstanceID    `json:"instance"`
	Timestamp Timestamp     `json:"timestamp"`
	Props     Props         `json:"props"`
}

// Kind returns KindREP.
func (REP) Kind() Kind { return KindREP }

// Validate checks the protocol version, the sender path, the interface name,
// the instance id and every property name.
func (m REP) Validate() error {
	if err := checkVersion(m.Version); err != nil {
		return err
	}
	if err := m.Sender.Validate(); err != nil {
		return fmt.Errorf("sender: %w", err)
	}
	if err := m.Interface.Validate(); err != nil {
		return err
	}
	if err := m.Instance.Validate(); err != nil {
		return err
	}
	return m.Props.validate()
}

// DES asks the receiver to set the properties in Props to the given values.
// The receiver does not change a property that Props leaves out. A DES travels
// downward to one node.
type DES struct {
	Version   uint          `json:"version"`
	Receiver  NodePath      `json:"receiver"`
	Interface InterfaceName `json:"interface"`
	Instance  InstanceID    `json:"instance"`
	Props     Props         `json:"props"`
}

// Kind returns KindDES.
func (DES) Kind() Kind { return KindDES }

// Validate checks the protocol version, the receiver path, the interface name,
// the instance id and every property name.
func (m DES) Validate() error {
	if err := checkVersion(m.Version); err != nil {
		return err
	}
	if err := m.Receiver.Validate(); err != nil {
		return fmt.Errorf("receiver: %w", err)
	}
	if err := m.Interface.Validate(); err != nil {
		return err
	}
	if err := m.Instance.Validate(); err != nil {
		return err
	}
	return m.Props.validate()
}

// RPC asks the receiver to run a method. It travels downward to one node. The
// receiver answers every RPC with a RES. Params is absent when the method
// takes no parameters.
type RPC struct {
	Version   uint              `json:"version"`
	ID        RPCID             `json:"id"`
	Sender    NodePath          `json:"sender"`
	Receiver  NodePath          `json:"receiver"`
	Interface InterfaceName     `json:"interface"`
	Instance  InstanceID        `json:"instance"`
	Method    MethodName        `json:"method"`
	Params    []json.RawMessage `json:"params,omitempty"`
	Expires   Timestamp         `json:"expires"`
}

// Kind returns KindRPC.
func (RPC) Kind() Kind { return KindRPC }

// Validate checks the protocol version, the RPC id, the sender and receiver
// paths, the interface name, the instance id and the method name.
func (m RPC) Validate() error {
	if err := checkVersion(m.Version); err != nil {
		return err
	}
	if err := m.ID.Validate(); err != nil {
		return err
	}
	if err := m.Sender.Validate(); err != nil {
		return fmt.Errorf("sender: %w", err)
	}
	if err := m.Receiver.Validate(); err != nil {
		return fmt.Errorf("receiver: %w", err)
	}
	if err := m.Interface.Validate(); err != nil {
		return err
	}
	if err := m.Instance.Validate(); err != nil {
		return err
	}
	return m.Method.Validate()
}

// Expired reports whether now is after the Expires time of the RPC. A node must
// not run or forward an expired RPC, and must check this before it reads the
// receiver field.
func (m RPC) Expired(now Timestamp) bool { return now > m.Expires }

// Error is the error field of a RES that answers a failed RPC. Code is required.
// Message is free text for a person and may be empty.
type Error struct {
	Message string `json:"message,omitempty"`
	Code    Code   `json:"code"`
}

// Error returns the code and, when there is one, the message.
func (e *Error) Error() string {
	if e.Message == "" {
		return e.Code.String()
	}
	return e.Code.String() + ": " + e.Message
}

// ErrResultWithError reports that a RES has both a result and an error. A
// failed call has no result.
var ErrResultWithError = errors.New("res carries a result and an error")

// RES answers an RPC. It travels upward to the node named in Receiver, the
// node that sent the RPC. Result is absent when the method returns nothing
// and when it failed. Error is absent when the method succeeded.
type RES struct {
	Version  uint            `json:"version"`
	ID       RPCID           `json:"id"`
	Receiver NodePath        `json:"receiver"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *Error          `json:"error,omitempty"`
}

// Kind returns KindRES.
func (RES) Kind() Kind { return KindRES }

// Failed reports whether the RES has an error, which means the method failed.
func (m RES) Failed() bool { return m.Error != nil }

// Validate checks the protocol version, the RPC id and the receiver path. For a
// RES with an error it also checks that Result is absent and that Valid is
// true for the error code.
func (m RES) Validate() error {
	if err := checkVersion(m.Version); err != nil {
		return err
	}
	if err := m.ID.Validate(); err != nil {
		return err
	}
	if err := m.Receiver.Validate(); err != nil {
		return fmt.Errorf("receiver: %w", err)
	}
	if m.Error == nil {
		return nil
	}
	if len(m.Result) > 0 {
		return ErrResultWithError
	}
	if !m.Error.Code.Valid() {
		return fmt.Errorf("error code %s: %w", m.Error.Code, ErrReservedCode)
	}
	return nil
}

// LOS says that the sender lost the connection to a node directly below it.
// Child is the Node ID of that node, so its path is Sender, a slash and Child.
// A LOS travels upward. Every node between the sender and the root processes
// it and forwards it unchanged.
type LOS struct {
	Version uint     `json:"version"`
	Sender  NodePath `json:"sender"`
	Child   NodeID   `json:"child"`
}

// Kind returns KindLOS.
func (LOS) Kind() Kind { return KindLOS }

// Affected returns the Node Path of the node that disconnected.
func (m LOS) Affected() (NodePath, error) { return m.Sender.Child(m.Child) }

// Validate checks the protocol version, the sender path and the Node ID in
// Child. It also checks that the path Affected returns is at most
// MaxNodePathLength characters.
func (m LOS) Validate() error {
	if err := checkVersion(m.Version); err != nil {
		return err
	}
	if err := m.Sender.Validate(); err != nil {
		return fmt.Errorf("sender: %w", err)
	}
	if err := m.Child.Validate(); err != nil {
		return fmt.Errorf("child: %w", err)
	}
	_, err := m.Affected()
	return err
}

// RBR asks every node below the sender to report the full state of its
// interfaces. It travels downward.
type RBR struct {
	Version uint `json:"version"`
}

// Kind returns KindRBR.
func (RBR) Kind() Kind { return KindRBR }

// Validate checks the protocol version.
func (m RBR) Validate() error { return checkVersion(m.Version) }

// AREP reports the state of one interface instance of the sender to the nodes
// below it. It has the fields and the JSON of REP and travels downward. Every
// node on the way processes it and forwards it unchanged.
type AREP REP

// Kind returns KindAREP.
func (AREP) Kind() Kind { return KindAREP }

// Validate checks the fields as REP.Validate does.
func (m AREP) Validate() error { return REP(m).Validate() }

// ALOS says that the sender lost the connection to the node directly above it.
// A receiver marks every node above the sender stale and leaves the sender as
// it is. An ALOS travels downward.
type ALOS struct {
	Version uint     `json:"version"`
	Sender  NodePath `json:"sender"`
}

// Kind returns KindALOS.
func (ALOS) Kind() Kind { return KindALOS }

// Validate checks the protocol version and the sender path.
func (m ALOS) Validate() error {
	if err := checkVersion(m.Version); err != nil {
		return err
	}
	if err := m.Sender.Validate(); err != nil {
		return fmt.Errorf("sender: %w", err)
	}
	return nil
}

// ARBR asks every node above the sender to report the full state of the
// interfaces it exposes downward. It travels upward.
type ARBR struct {
	Version uint `json:"version"`
}

// Kind returns KindARBR.
func (ARBR) Kind() Kind { return KindARBR }

// Validate checks the protocol version.
func (m ARBR) Validate() error { return checkVersion(m.Version) }
