package amqp

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// How this suite tells its own broker from any other broker on the same port.
//
// An open socket says who is listening and never who answered. Any stack on
// this host may publish a broker of its own on the same port, and an AMQP
// broker that is not this fixture accepts every publish the suite makes, so
// the run goes green against somebody else's data.
//
// The first frame a broker sends is Connection.Start, and its server-properties
// table names the product and the version. That is read before the handshake
// goes any further and compared with the image composeFile declares.
const (
	// composeFile declares which broker the fixture runs. It is what separates
	// this repository's broker from any other broker reachable at the address.
	composeFile = "docker/rabbitmq-fixture/rabbitmq-fixture.yml"

	// composeFilePath reaches composeFile from this package's directory.
	composeFilePath = "../../../" + composeFile

	// fixtureProduct is the product name RabbitMQ puts in server-properties.
	fixtureProduct = "RabbitMQ"
)

// identityTimeout bounds the identity read. Connection.Start is the broker's
// first frame, so a broker that sends nothing in this window speaks no AMQP.
const identityTimeout = 10 * time.Second

// ErrForeignBroker reports a broker that is not the one composeFile declares.
var ErrForeignBroker = errors.New("amqp: the broker on this port is not this repository's fixture")

// fixtureImage matches the rabbitmq image tag composeFile names, capturing the
// version and dropping the image's flavour suffix.
var fixtureImage = regexp.MustCompile(`(?m)^\s*image:\s*rabbitmq:([0-9][0-9.]*)`)

// declaredVersion reads the RabbitMQ version composeFile pins. Every case
// compares against the same value, so the file is read once.
var declaredVersion = sync.OnceValues(func() (string, error) {
	raw, err := os.ReadFile(composeFilePath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", composeFile, err)
	}

	return declaredVersionFrom(raw)
})

// declaredVersionFrom pulls the RabbitMQ version out of a compose file.
func declaredVersionFrom(raw []byte) (string, error) {
	found := fixtureImage.FindSubmatch(raw)
	if found == nil {
		return "", fmt.Errorf("%s names no rabbitmq image", composeFile)
	}

	return string(found[1]), nil
}

// serverProperties reads the broker's own name and version once per run. Every
// case that needs a broker asks for it, so the extra connection is opened once.
var serverProperties = sync.OnceValues(func() (map[string]string, error) {
	return readServerProperties(fixtureAddress())
})

// checkBrokerIsOurs reports whether the broker at address is the one composeFile
// declares. properties is what it sent in server-properties.
func checkBrokerIsOurs(address string, properties map[string]string, declared string) error {
	if declared == "" {
		return fmt.Errorf("%s declares no rabbitmq version", composeFile)
	}

	product, version := properties["product"], properties["version"]
	if product == fixtureProduct && version == declared {
		return nil
	}

	said := fmt.Sprintf("announces %q version %q", product, version)
	if product == "" && version == "" {
		said = "names neither a product nor a version in server-properties"
	}

	return foreignBroker(address, said, declared)
}

// foreignBroker is what a broker that is not the fixture reads as. It names
// what answered, what composeFile declares, and the two commands that move this
// repository's broker to a free port.
func foreignBroker(address, said, declared string) error {
	return fmt.Errorf(
		"%w.\n"+
			"the broker at %s %s.\n"+
			"%s declares %s %s.\n"+
			"another service holds that port. start this repository's broker on a free port,"+
			" then point the suite at it:\n"+
			"    AMQP_FIXTURE_PORT=<free port> docker compose -f docker-compose.local.yml up -d rabbitmq-fixture\n"+
			"    PCL_AMQP_TEST_BROKER=127.0.0.1:<free port> go test ./adapters/events/amqp/",
		ErrForeignBroker, address, said, composeFile, fixtureProduct, declared)
}

// readServerProperties opens a connection, reads Connection.Start and stops
// there. The handshake goes no further because the identity is settled by the
// broker's first frame, and a broker that is not this one must not be
// authenticated against.
func readServerProperties(address string) (map[string]string, error) {
	socket, err := net.DialTimeout("tcp", address, identityTimeout)
	if err != nil {
		return nil, err
	}

	defer func() { _ = socket.Close() }()

	if err = socket.SetDeadline(time.Now().Add(identityTimeout)); err != nil {
		return nil, err
	}

	if _, err = socket.Write(protocolHeader); err != nil {
		return nil, err
	}

	start, err := expect(bufio.NewReader(socket), classConnection, connectionStart)
	if err != nil {
		return nil, err
	}

	start.args.octet() // version-major
	start.args.octet() // version-minor

	properties, err := tableStrings(start.args)
	if err != nil {
		return nil, err
	}

	if len(properties) == 0 {
		return nil, fmt.Errorf("%w: the broker sent an empty server-properties table", ErrProtocol)
	}

	return properties, nil
}

// TestTheSuiteRefusesABrokerThatIsNotTheFixture drives the guard both ways,
// because a guard that only proves server-properties arrived passes against any
// broker that sends them.
func TestTheSuiteRefusesABrokerThatIsNotTheFixture(t *testing.T) {
	t.Parallel()

	declared, err := declaredVersion()
	require.NoError(t, err)
	require.NotEmpty(t, declared, "%s declares no rabbitmq version", composeFile)

	address := fixtureAddress()

	ours := map[string]string{"product": fixtureProduct, "version": declared, "platform": "Erlang/OTP 27"}
	require.NoError(t, checkBrokerIsOurs(address, ours, declared),
		"the broker %s pins must be accepted", composeFile)

	// Another RabbitMQ, at another version, is another broker.
	older := map[string]string{"product": fixtureProduct, "version": "3.13.7"}
	require.NotEqual(t, declared, older["version"], "%s now pins the version this case calls foreign", composeFile)

	err = checkBrokerIsOurs(address, older, declared)
	require.ErrorIs(t, err, ErrForeignBroker, "a broker announcing another version must be refused")
	require.Contains(t, err.Error(), address)
	require.Contains(t, err.Error(), older["version"])
	require.Contains(t, err.Error(), declared)
	t.Logf("another RabbitMQ reads:\n%v", err)

	// A broker of another make. Every AMQP 0-9-1 broker answers the handshake
	// and accepts the suite's publishes.
	other := map[string]string{"product": "LavinMQ", "version": declared}
	require.ErrorIs(t, checkBrokerIsOurs(address, other, declared), ErrForeignBroker,
		"a broker of another make must be refused")

	// A broker that names neither.
	err = checkBrokerIsOurs(address, map[string]string{"platform": "Erlang/OTP 27"}, declared)
	require.ErrorIs(t, err, ErrForeignBroker, "a broker naming neither must be refused")
	require.Contains(t, err.Error(), "names neither a product nor a version")
	t.Logf("a broker naming neither reads:\n%v", err)

	// A compose file that names no image leaves the guard with nothing to
	// compare against, and passing there is how this class of gate goes green.
	require.Error(t, checkBrokerIsOurs(address, ours, ""),
		"an unreadable declaration must refuse rather than pass")
}

// TestTheDeclaredVersionComesFromTheComposeFile holds the reading half. The
// guard compares against what the fixture pins, so a pattern that stops
// matching the compose file turns every broker foreign.
func TestTheDeclaredVersionComesFromTheComposeFile(t *testing.T) {
	t.Parallel()

	declared, err := declaredVersion()
	require.NoError(t, err)

	raw, err := os.ReadFile(composeFilePath)
	require.NoError(t, err)
	require.Contains(t, string(raw), "rabbitmq:"+declared,
		"%s no longer pins the version the guard read", composeFile)

	_, err = declaredVersionFrom([]byte("services:\n  rabbitmq-fixture:\n    image: some/other:1\n"))
	require.Error(t, err, "a compose file naming no rabbitmq image must be an error")
}

// TestServerPropertiesAreReadOutOfAWholeTable holds the walk. The product and
// the version sit at the end of RabbitMQ's table, so every field type before
// them has to be stepped over at the right width, and a width that is wrong by
// one reads the rest as nonsense.
func TestServerPropertiesAreReadOutOfAWholeTable(t *testing.T) {
	t.Parallel()

	table := encodeTable(
		encodeField("capabilities", 'F', encodeTable(encodeField("publisher_confirms", 't', []byte{1}))),
		encodeField("cluster_name", 'S', encodeLongString("rabbit@fixture")),
		encodeField("copyright", 'S', encodeLongString("Copyright (c) 2007-2026 Broadcom Inc.")),
		encodeField("platform", 'S', encodeLongString("Erlang/OTP 27")),
		encodeField("product", 'S', encodeLongString("RabbitMQ")),
		encodeField("version", 'S', encodeLongString("4.3.5")),
	)

	read, err := tableStrings(&reader{buf: table})
	require.NoError(t, err)
	require.Equal(t, "RabbitMQ", read["product"])
	require.Equal(t, "4.3.5", read["version"])
	require.Equal(t, "rabbit@fixture", read["cluster_name"])

	// A field type the walk cannot size stops it, because a guessed width reads
	// the fields after it as nonsense and could name any product at all.
	unknown := encodeTable(encodeField("product", 'Z', []byte{0}))
	_, err = tableStrings(&reader{buf: unknown})
	require.ErrorIs(t, err, ErrProtocol, "an unknown field type must refuse rather than guess")
}

// encodeField renders one field table entry: its name, its type and its value.
func encodeField(name string, kind byte, value []byte) []byte {
	out := append([]byte{byte(len(name))}, name...)
	out = append(out, kind)

	return append(out, value...)
}

// encodeTable renders a field table, which is its entries behind a byte count.
func encodeTable(entries ...[]byte) []byte {
	var body []byte
	for _, one := range entries {
		body = append(body, one...)
	}

	return append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...)
}

// encodeLongString renders a string behind a four byte count.
func encodeLongString(v string) []byte {
	return append(binary.BigEndian.AppendUint32(nil, uint32(len(v))), v...)
}

// The AMQP field types this reads. The value widths are the specification's,
// and an octet outside this set stops the walk rather than guessing a width,
// because a wrong width reads the rest of the table as nonsense.
const (
	fieldBool  = 't'
	fieldByte  = 'b'
	fieldUByte = 'B'
	// RabbitMQ encodes 's' as a signed 16 bit integer. The 0-9-1 grammar reads
	// it as a short string, and the width is what a walk needs, so the broker
	// under test decides it.
	fieldShort     = 's'
	fieldUShort    = 'u'
	fieldInt       = 'I'
	fieldUInt      = 'i'
	fieldFloat     = 'f'
	fieldLong      = 'L'
	fieldULong     = 'l'
	fieldDouble    = 'd'
	fieldTimestamp = 'T'
	fieldDecimal   = 'D'
	fieldLongStr   = 'S'
	fieldArray     = 'A'
	fieldBytes     = 'x'
	fieldTable     = 'F'
	fieldVoid      = 'V'
)

// tableStrings walks a field table and keeps the entries that are strings. The
// product and the version are two of them, and the rest of the table is walked
// because a field's width has to be known to reach the field after it.
func tableStrings(r *reader) (map[string]string, error) {
	size := int(r.long())
	if r.err != nil {
		return nil, r.err
	}

	if size > len(r.buf) {
		return nil, fmt.Errorf("%w: server-properties announced %d bytes and %d were left",
			ErrProtocol, size, len(r.buf))
	}

	table := &reader{buf: r.buf[:size]}
	r.buf = r.buf[size:]

	found := map[string]string{}

	for len(table.buf) > 0 {
		name := table.shortstr()

		value, isString, err := fieldValue(table)
		if err != nil {
			return nil, err
		}

		if table.err != nil {
			return nil, table.err
		}

		if isString {
			found[name] = value
		}
	}

	return found, nil
}

// fieldValue steps over one field and reports its value where that value is a
// string.
func fieldValue(table *reader) (value string, isString bool, err error) {
	const (
		decimalBytes = 5
		twoBytes     = 2
		fourBytes    = 4
		eightBytes   = 8
	)

	switch kind := table.octet(); kind {
	case fieldLongStr:
		return table.longstr(), true, nil
	case fieldBool, fieldByte, fieldUByte:
		table.octet()
	case fieldShort, fieldUShort:
		skip(table, twoBytes)
	case fieldInt, fieldUInt, fieldFloat:
		skip(table, fourBytes)
	case fieldLong, fieldULong, fieldDouble, fieldTimestamp:
		skip(table, eightBytes)
	case fieldDecimal:
		skip(table, decimalBytes)
	case fieldArray, fieldBytes, fieldTable:
		skip(table, int(table.long()))
	case fieldVoid:
	default:
		return "", false, fmt.Errorf("%w: server-properties carries field type %q", ErrProtocol, kind)
	}

	return "", false, nil
}

// skip steps over a value of a known width.
func skip(r *reader, width int) {
	if r.err != nil || width < 0 || len(r.buf) < width {
		r.fail(width)

		return
	}

	r.buf = r.buf[width:]
}
