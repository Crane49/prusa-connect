package kafka

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// How this suite tells its own cluster from any other cluster on the same port.
//
// An open socket says who is listening and never who answered. Any stack on
// this host may publish a broker of its own on the same port, and a
// Kafka-protocol cluster that is not this fixture accepts every record the
// suite produces, so the run goes green against somebody else's data.
//
// A Metadata response names the cluster: how many brokers it holds, the node id
// of each, and the address each advertises. composeFile fixes all three for the
// fixture, at --node-id and --advertise-kafka-addr, so what a cluster answers is
// compared with what the file declares.
const (
	// composeFile declares which cluster the fixture runs. It is what separates
	// this repository's cluster from any other cluster reachable at the address.
	composeFile = "docker/redpanda-fixture/redpanda-fixture.yml"

	// composeFilePath reaches composeFile from this package's directory.
	composeFilePath = "../../../" + composeFile

	// envFile holds the port overrides compose interpolates into composeFile.
	envFile = ".env"

	// envFilePath reaches envFile from this package's directory.
	envFilePath = "../../../" + envFile
)

// identityTimeout bounds the identity read. Metadata is the first request a
// producer makes, so a cluster that says nothing in this window answers no
// producer either.
const identityTimeout = 10 * time.Second

// ErrForeignCluster reports a cluster that is not the one composeFile declares.
var ErrForeignCluster = errors.New("kafka: the cluster on this port is not this repository's fixture")

// The two command line arguments composeFile fixes the fixture's identity with.
var (
	fixtureNode      = regexp.MustCompile(`--node-id=([0-9]+)`)
	fixtureAdvertise = regexp.MustCompile(`--advertise-kafka-addr=PLAINTEXT://(\S+)`)

	// interpolation matches the `${NAME:-default}` compose expands before the
	// container reads the argument.
	interpolation = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*):-([^}]*)\}`)
)

// fixtureCluster is what composeFile declares the cluster to be.
type fixtureCluster struct {
	node      int32
	advertise string
}

// clusterMember is one broker as a Metadata response describes it.
type clusterMember struct {
	node    int32
	address string
}

// clusterDescription is a cluster as it describes itself: which brokers it
// holds, and the name it answers to.
type clusterDescription struct {
	members []clusterMember
	name    string
}

// declaredCluster reads the fixture's node id and advertised address out of
// composeFile. Every case compares against the same values, so it is read once.
var declaredCluster = sync.OnceValues(func() (fixtureCluster, error) {
	raw, err := os.ReadFile(composeFilePath)
	if err != nil {
		return fixtureCluster{}, fmt.Errorf("read %s: %w", composeFile, err)
	}

	return declaredClusterFrom(raw, composeEnv)
})

// declaredClusterFrom pulls the two arguments out of a compose file, expanding
// the interpolation compose would have expanded.
func declaredClusterFrom(raw []byte, lookup func(name string) (string, bool)) (fixtureCluster, error) {
	node := fixtureNode.FindSubmatch(raw)
	if node == nil {
		return fixtureCluster{}, fmt.Errorf("%s names no --node-id", composeFile)
	}

	id, err := strconv.ParseInt(string(node[1]), 10, 32)
	if err != nil {
		return fixtureCluster{}, fmt.Errorf("%s names node id %q: %w", composeFile, node[1], err)
	}

	advertise := fixtureAdvertise.FindSubmatch(raw)
	if advertise == nil {
		return fixtureCluster{}, fmt.Errorf("%s names no --advertise-kafka-addr", composeFile)
	}

	return fixtureCluster{node: int32(id), advertise: expand(string(advertise[1]), lookup)}, nil
}

// expand replaces every `${NAME:-default}` with what compose would put there.
func expand(raw string, lookup func(name string) (string, bool)) string {
	return interpolation.ReplaceAllStringFunc(raw, func(match string) string {
		parts := interpolation.FindStringSubmatch(match)

		if value, ok := lookup(parts[1]); ok && value != "" {
			return value
		}

		return parts[2]
	})
}

// composeEnv resolves a variable the way compose does: the process environment
// first, then envFile, and the argument's own default where neither holds it.
func composeEnv(name string) (string, bool) {
	if value, ok := os.LookupEnv(name); ok {
		return value, true
	}

	raw, err := os.ReadFile(envFilePath)
	if err != nil {
		return "", false
	}

	for line := range strings.Lines(string(raw)) {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && key == name {
			return strings.Trim(value, `"'`), true
		}
	}

	return "", false
}

// describedCluster reads the cluster's own description once per run. Every case
// that needs a cluster asks for it, so the extra connection is opened once.
var describedCluster = sync.OnceValues(func() (clusterDescription, error) {
	return readClusterDescription(fixtureAddress())
})

// checkClusterIsOurs reports whether the cluster at address is the one
// composeFile declares. described is what Metadata answered.
func checkClusterIsOurs(address string, described clusterDescription, declared fixtureCluster) error {
	if declared.advertise == "" {
		return fmt.Errorf("%s declares no advertised address", composeFile)
	}

	only := len(described.members) == 1
	if only && described.members[0].node == declared.node && described.members[0].address == declared.advertise {
		return nil
	}

	said := "holds " + memberList(described.members)
	if described.name != "" {
		said += ", calling itself " + strconv.Quote(described.name)
	}

	return foreignCluster(address, said, declared)
}

// memberList renders a cluster's brokers for a failure message.
func memberList(members []clusterMember) string {
	if len(members) == 0 {
		return "no broker at all"
	}

	rendered := make([]string, 0, len(members))
	for _, one := range members {
		rendered = append(rendered, fmt.Sprintf("node %d at %s", one.node, one.address))
	}

	return strings.Join(rendered, ", ")
}

// foreignCluster is what a cluster that is not the fixture reads as. It names
// what answered, what composeFile declares, and the two commands that move this
// repository's cluster to a free port.
func foreignCluster(address, said string, declared fixtureCluster) error {
	return fmt.Errorf(
		"%w.\n"+
			"the cluster at %s %s.\n"+
			"%s declares one broker, node %d at %s.\n"+
			"another service holds that port. start this repository's cluster on a free port,"+
			" then point the suite at it:\n"+
			"    KAFKA_FIXTURE_PORT=<free port> docker compose -f docker-compose.local.yml up -d redpanda-fixture\n"+
			"    PCL_KAFKA_TEST_BROKER=127.0.0.1:<free port> go test ./adapters/events/kafka/",
		ErrForeignCluster, address, said, composeFile, declared.node, declared.advertise)
}

// TestTheSuiteRefusesAClusterThatIsNotTheFixture drives the guard both ways,
// because a guard that only proves a Metadata response arrived passes against
// any cluster that sends one.
func TestTheSuiteRefusesAClusterThatIsNotTheFixture(t *testing.T) {
	t.Parallel()

	declared, err := declaredCluster()
	require.NoError(t, err)
	require.NotEmpty(t, declared.advertise, "%s declares no advertised address", composeFile)

	address := fixtureAddress()

	ours := clusterDescription{
		members: []clusterMember{{node: declared.node, address: declared.advertise}},
		name:    "redpanda.f2f47a13",
	}
	require.NoError(t, checkClusterIsOurs(address, ours, declared),
		"the cluster %s declares must be accepted", composeFile)

	// A cluster of the right kind on the same port, advertising itself
	// elsewhere. It accepts every record this suite produces.
	elsewhere := clusterDescription{members: []clusterMember{{node: declared.node, address: "127.0.0.1:9092"}}}
	require.NotEqual(t, declared.advertise, elsewhere.members[0].address,
		"%s now advertises the address this case calls foreign", composeFile)

	err = checkClusterIsOurs(address, elsewhere, declared)
	require.ErrorIs(t, err, ErrForeignCluster, "a cluster advertising another address must be refused")
	require.Contains(t, err.Error(), address)
	require.Contains(t, err.Error(), "127.0.0.1:9092")
	require.Contains(t, err.Error(), declared.advertise)
	t.Logf("a cluster advertising elsewhere reads:\n%v", err)

	// A cluster of more than one broker is not this fixture, which runs one.
	crowd := clusterDescription{members: []clusterMember{
		{node: declared.node, address: declared.advertise},
		{node: declared.node + 1, address: "127.0.0.1:9093"},
	}}
	require.ErrorIs(t, checkClusterIsOurs(address, crowd, declared), ErrForeignCluster,
		"a cluster of several brokers must be refused")

	// A broker under another node id is another broker.
	renumbered := clusterDescription{members: []clusterMember{{node: declared.node + 7, address: declared.advertise}}}
	require.ErrorIs(t, checkClusterIsOurs(address, renumbered, declared), ErrForeignCluster,
		"a broker under another node id must be refused")

	// A response describing no broker at all.
	err = checkClusterIsOurs(address, clusterDescription{}, declared)
	require.ErrorIs(t, err, ErrForeignCluster, "a cluster describing no broker must be refused")
	require.Contains(t, err.Error(), "no broker at all")
	t.Logf("a cluster describing no broker reads:\n%v", err)

	// A compose file that names no advertised address leaves the guard with
	// nothing to compare against, and passing there is how this class of gate
	// goes green.
	require.Error(t, checkClusterIsOurs(address, ours, fixtureCluster{}),
		"an unreadable declaration must refuse rather than pass")
}

// TestTheDeclaredClusterComesFromTheComposeFile holds the reading half. The
// guard compares against what the fixture declares, so a pattern that stops
// matching the compose file turns every cluster foreign.
func TestTheDeclaredClusterComesFromTheComposeFile(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(composeFilePath)
	require.NoError(t, err)

	// The port compose interpolates comes from the environment, then .env, then
	// the argument's own default, and the guard has to resolve it the same way
	// or it refuses the fixture it is looking for.
	unset := func(string) (string, bool) { return "", false }

	fallback, err := declaredClusterFrom(raw, unset)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:59092", fallback.advertise, "%s no longer defaults to 59092", composeFile)
	require.Equal(t, int32(0), fallback.node, "%s no longer runs one broker as node 0", composeFile)

	chosen, err := declaredClusterFrom(raw, func(name string) (string, bool) {
		return "51234", name == "KAFKA_FIXTURE_PORT"
	})
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:51234", chosen.advertise, "an overridden port must reach the guard")

	_, err = declaredClusterFrom([]byte("services:\n  redpanda-fixture:\n    command: [redpanda, start]\n"), unset)
	require.Error(t, err, "a compose file fixing no node id must be an error")
}

// TestAClusterIsReadOutOfARealMetadataResponse holds the decode. A field read
// at the wrong width reads the fields after it as nonsense, and the identity
// then names whatever the misread produced.
func TestAClusterIsReadOutOfARealMetadataResponse(t *testing.T) {
	t.Parallel()

	body := &encoder{}
	body.int32(0) // throttle time
	body.int32(2) // two brokers
	body.int32(0)
	body.text("127.0.0.1")
	body.int32(59092)
	body.nullableText("", false) // no rack
	body.int32(1)
	body.text("broker-two")
	body.int32(9092)
	body.nullableText("eu-west", true)
	body.nullableText("redpanda.f2f47a13", true) // cluster id
	body.int32(0)                                // controller id
	body.int32(0)                                // no topics

	described, err := decodeClusterDescription(&decoder{buf: body.buf})
	require.NoError(t, err)
	require.Equal(t, "redpanda.f2f47a13", described.name)
	require.Equal(t, []clusterMember{
		{node: 0, address: "127.0.0.1:59092"},
		{node: 1, address: "broker-two:9092"},
	}, described.members)

	// A response that ends early must refuse rather than report the brokers it
	// managed to read, because a short read is a cluster this suite cannot name.
	_, err = decodeClusterDescription(&decoder{buf: body.buf[:12]})
	require.ErrorIs(t, err, ErrProtocol, "a truncated response must refuse rather than pass")
}

// readClusterDescription asks for the cluster's description and reads the
// brokers and the cluster id out of it. The topic array is empty, so the
// request creates nothing and describes only the cluster.
func readClusterDescription(address string) (clusterDescription, error) {
	socket, err := net.DialTimeout("tcp", address, identityTimeout)
	if err != nil {
		return clusterDescription{}, err
	}

	defer func() { _ = socket.Close() }()

	if err = socket.SetDeadline(time.Now().Add(identityTimeout)); err != nil {
		return clusterDescription{}, err
	}

	body := &encoder{}
	body.int32(0)       // no topics, so nothing is described and nothing is created
	body.boolean(false) // and nothing is auto-created either

	const correlation = 1

	if _, err = socket.Write(request(apiMetadata, metadataVersion, correlation, "identity", body.buf)); err != nil {
		return clusterDescription{}, err
	}

	answer, err := readResponse(bufio.NewReader(socket), correlation)
	if err != nil {
		return clusterDescription{}, err
	}

	return decodeClusterDescription(answer)
}

// decodeClusterDescription reads the brokers and the cluster id out of a
// Metadata v4 response.
func decodeClusterDescription(answer *decoder) (clusterDescription, error) {
	answer.int32() // throttle time

	var described clusterDescription

	for range answer.arrayLen() {
		node := answer.int32()
		host := answer.text()
		port := answer.int32()
		answer.text() // rack

		described.members = append(described.members,
			clusterMember{node: node, address: net.JoinHostPort(host, strconv.Itoa(int(port)))})
	}

	described.name = answer.text()

	if answer.err != nil {
		return clusterDescription{}, answer.err
	}

	if len(described.members) == 0 {
		return clusterDescription{}, fmt.Errorf("%w: the cluster described no broker at all", ErrProtocol)
	}

	return described, nil
}
