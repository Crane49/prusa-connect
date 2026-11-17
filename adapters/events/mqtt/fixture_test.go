package mqtt

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
// this host may publish a broker of its own on the same port, and an MQTT
// broker that is not this fixture accepts every publish the suite makes, so
// the run goes green against somebody else's data.
//
// Mosquitto publishes its own name and version on $SYS/broker/version and
// retains it, so a subscriber reads it at connect time. That is compared with
// the image imageFile pins. The fixture's healthcheck subscribes to
// $SYS/broker/uptime, so the tree is published under this configuration.
const (
	// imageFile builds the broker the fixture runs, FROM a mosquitto image whose
	// tag names the version beside the digest. It is what separates
	// this repository's broker from any other broker reachable at the address.
	imageFile = "docker/mosquitto-fixture/Dockerfile"

	// imageFilePath reaches imageFile from this package's directory.
	imageFilePath = "../../../" + imageFile

	// versionTopic carries the broker's own name and version.
	versionTopic = "$SYS/broker/version"
)

// identityTimeout bounds the identity read. A retained message is delivered
// with the subscription acknowledgement, so a broker that says nothing in this
// window publishes no $SYS tree at all.
const identityTimeout = 5 * time.Second

// ErrForeignBroker reports a broker that is not the one imageFile declares.
var ErrForeignBroker = errors.New("mqtt: the broker on this port is not this repository's fixture")

// fixtureImage matches the mosquitto image tag imageFile builds FROM, capturing the
// version and dropping the image's flavour suffix and its digest.
var fixtureImage = regexp.MustCompile(`(?m)^FROM\s+eclipse-mosquitto:([0-9][0-9.]*)`)

// announcedVersion matches what mosquitto retains on versionTopic.
var announcedVersion = regexp.MustCompile(`(?i)^mosquitto version (\S+)`)

// declaredVersion reads the mosquitto version imageFile pins. Every case
// compares against the same value, so the file is read once.
var declaredVersion = sync.OnceValues(func() (string, error) {
	raw, err := os.ReadFile(imageFilePath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", imageFile, err)
	}

	return declaredVersionFrom(raw)
})

// declaredVersionFrom pulls the mosquitto version out of a Dockerfile.
func declaredVersionFrom(raw []byte) (string, error) {
	found := fixtureImage.FindSubmatch(raw)
	if found == nil {
		return "", fmt.Errorf("%s names no eclipse-mosquitto image", imageFile)
	}

	return string(found[1]), nil
}

// brokerVersion reads the broker's own version once per run. Every case that
// needs a broker asks for it, and a broker that says nothing costs
// identityTimeout, so the read is paid for once.
var brokerVersion = sync.OnceValues(func() (string, error) { return readVersion(fixtureAddress()) })

// checkBrokerIsOurs reports whether the broker at address is the one imageFile
// declares. announced is what it retained on versionTopic, and the empty string
// is a broker that retained nothing there.
func checkBrokerIsOurs(address, announced, declared string) error {
	if declared == "" {
		return fmt.Errorf("%s declares no mosquitto version", imageFile)
	}

	found := announcedVersion.FindStringSubmatch(announced)
	if found != nil && found[1] == declared {
		return nil
	}

	said := fmt.Sprintf("announces %q on %s", announced, versionTopic)
	if announced == "" {
		said = "announces nothing on " + versionTopic
	}

	return foreignBroker(address, said, declared)
}

// foreignBroker is what a broker that is not the fixture reads as. It names
// what answered, what imageFile declares, and the two commands that move this
// repository's broker to a free port.
func foreignBroker(address, said, declared string) error {
	return fmt.Errorf(
		"%w.\n"+
			"the broker at %s %s.\n"+
			"%s declares mosquitto %s.\n"+
			"another service holds that port. start this repository's broker on a free port,"+
			" then point the suite at it:\n"+
			"    MQTT_FIXTURE_PORT=<free port> docker compose -f docker-compose.local.yml up -d mosquitto-fixture\n"+
			"    PCL_MQTT_TEST_BROKER=127.0.0.1:<free port> go test ./adapters/events/mqtt/",
		ErrForeignBroker, address, said, imageFile, declared)
}

// readVersion subscribes to versionTopic and reports what the broker retained
// there, whatever that says. A broker that answers nothing within
// identityTimeout produces an error, because an unread identity proves as
// little as an open socket.
func readVersion(address string) (string, error) {
	socket, err := net.DialTimeout("tcp", address, identityTimeout)
	if err != nil {
		return "", err
	}

	defer func() { _ = socket.Close() }()

	if err = socket.SetDeadline(time.Now().Add(identityTimeout)); err != nil {
		return "", err
	}

	reader := bufio.NewReader(socket)

	clientID := uniqueName("identity-")

	connect, err := encodeConnect(clientID, 0, credentials{}, will{})
	if err != nil {
		return "", err
	}

	if _, err = socket.Write(connect); err != nil {
		return "", err
	}

	kind, _, body, err := readPacket(reader)
	if err != nil {
		return "", err
	}

	if kind != connackPacket {
		return "", fmt.Errorf("the first packet back was type %d and not a connack", kind)
	}

	if err = connackError(body); err != nil {
		return "", err
	}

	if _, err = socket.Write(encodeSubscribe(1, versionTopic)); err != nil {
		return "", err
	}

	return awaitVersion(reader)
}

// awaitVersion reads until the retained message on versionTopic arrives, or
// until the socket deadline ends the read.
func awaitVersion(reader *bufio.Reader) (string, error) {
	for {
		kind, flags, body, err := readPacket(reader)
		if err != nil {
			return "", err
		}

		if kind != publishPacket {
			continue
		}

		topic, payload, ok := publishFields(flags, body)
		if !ok || topic != versionTopic {
			continue
		}

		return payload, nil
	}
}

// TestTheSuiteRefusesABrokerThatIsNotTheFixture drives the guard both ways,
// because a guard that only proves a version was announced passes against any
// broker that announces one.
func TestTheSuiteRefusesABrokerThatIsNotTheFixture(t *testing.T) {
	t.Parallel()

	declared, err := declaredVersion()
	require.NoError(t, err)
	require.NotEmpty(t, declared, "%s declares no mosquitto version", imageFile)

	address := fixtureAddress()

	require.NoError(t, checkBrokerIsOurs(address, "mosquitto version "+declared, declared),
		"the version %s pins must be accepted", imageFile)

	// Another mosquitto, at another version, is another broker.
	older := "2.0.18"
	require.NotEqual(t, declared, older, "%s now pins the version this case calls foreign", imageFile)

	err = checkBrokerIsOurs(address, "mosquitto version "+older, declared)
	require.ErrorIs(t, err, ErrForeignBroker, "a broker announcing another version must be refused")
	require.Contains(t, err.Error(), address)
	require.Contains(t, err.Error(), older)
	require.Contains(t, err.Error(), declared)
	t.Logf("another mosquitto reads:\n%v", err)

	// What a broker that is not mosquitto retains on the same topic. Mochi
	// answers with its own version and nothing else, which is why a topic that
	// carries a value is not on its own an identity.
	err = checkBrokerIsOurs(address, "2.7.9", declared)
	require.ErrorIs(t, err, ErrForeignBroker, "a broker that names no product must be refused")
	require.Contains(t, err.Error(), "2.7.9")
	t.Logf("a broker of another make reads:\n%v", err)

	// A broker that publishes no $SYS tree at all.
	err = checkBrokerIsOurs(address, "", declared)
	require.ErrorIs(t, err, ErrForeignBroker, "a broker announcing nothing must be refused")
	require.Contains(t, err.Error(), "announces nothing")
	t.Logf("a broker with no $SYS tree reads:\n%v", err)

	// A version the declared one is a prefix of is a different version.
	require.ErrorIs(t, checkBrokerIsOurs(address, "mosquitto version "+declared+".1", declared), ErrForeignBroker,
		"a version the declared one is a prefix of must be refused")

	// A Dockerfile that names no tagged image leaves the guard with nothing to
	// compare against, and passing there is how this class of gate goes green.
	require.Error(t, checkBrokerIsOurs(address, "mosquitto version "+declared, ""),
		"an unreadable declaration must refuse rather than pass")
}

// TestTheDeclaredVersionComesFromTheDockerfile holds the reading half. The
// guard compares against what the fixture pins, so a pattern that stops
// matching the Dockerfile turns every broker foreign.
func TestTheDeclaredVersionComesFromTheDockerfile(t *testing.T) {
	t.Parallel()

	declared, err := declaredVersion()
	require.NoError(t, err)

	raw, err := os.ReadFile(imageFilePath)
	require.NoError(t, err)
	require.Contains(t, string(raw), "FROM eclipse-mosquitto:"+declared,
		"%s no longer pins the version the guard read", imageFile)

	_, err = declaredVersionFrom([]byte("FROM some/other:1\n"))
	require.Error(t, err, "a Dockerfile building from another image must be an error")

	// A digest alone is a pin the broker cannot be compared against: mosquitto
	// announces a version, never a digest, which is why the tag stays beside it.
	digestOnly := "FROM eclipse-mosquitto@sha256:6f8d8a947c506f8a2290ec65cd4bd2bc7cb4d43fb5f6271f861cb013e2ef9797\n"
	_, err = declaredVersionFrom([]byte(digestOnly))
	require.Error(t, err, "a Dockerfile pinning only a digest must be an error")
}

// publishFields splits a publish packet into its topic and its payload. The
// subscription is at QoS 0 and a retained $SYS message is published at QoS 0,
// so a packet identifier is read past rather than acknowledged.
func publishFields(flags byte, body []byte) (topic, payload string, ok bool) {
	const qosMask = 0x03

	if len(body) < 2 {
		return "", "", false
	}

	length := int(binary.BigEndian.Uint16(body))
	if len(body) < 2+length {
		return "", "", false
	}

	topic = string(body[2 : 2+length])
	rest := body[2+length:]

	if qos := (flags >> 1) & qosMask; qos > 0 {
		if len(rest) < 2 {
			return "", "", false
		}

		rest = rest[2:]
	}

	return topic, string(rest), true
}
