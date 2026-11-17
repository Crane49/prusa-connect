package kafka

import (
	"bufio"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Fetch is what a consumer does and this adapter does not, so its api key and
// its decoding live here: a reference implementation carries the code it runs,
// and a copier reading it should not have to work out which half is the test's.
const (
	apiFetch     = 1
	fetchVersion = 4
)

// consumed is one record as the cluster handed it back.
type consumed struct {
	partition int32
	key       string
	value     string
}

// fetchAll reads every record in a topic from the beginning, so a case can
// assert on what the cluster actually stored rather than on the bytes the
// adapter's own encoder produced.
func fetchAll(t *testing.T, address, topic string, partitions []int32, within time.Duration) []consumed {
	t.Helper()

	socket, err := net.DialTimeout("tcp", address, 5*time.Second)
	require.NoError(t, err)

	defer func() { _ = socket.Close() }()

	in := bufio.NewReader(socket)

	deadline := time.Now().Add(within)

	var seen []consumed

	for time.Now().Before(deadline) {
		seen = nil

		for _, id := range partitions {
			seen = append(seen, fetchPartition(t, socket, in, topic, id)...)
		}

		if len(seen) > 0 {
			return seen
		}

		time.Sleep(100 * time.Millisecond)
	}

	return seen
}

func fetchPartition(t *testing.T, socket net.Conn, in *bufio.Reader, topic string, id int32) []consumed {
	t.Helper()

	body := &encoder{}
	body.int32(-1)      // replica id, a consumer is not a broker
	body.int32(500)     // max wait
	body.int32(1)       // min bytes
	body.int32(1 << 20) // max bytes
	body.int8(0)        // isolation level: read uncommitted
	body.int32(1)       // one topic
	body.text(topic)
	body.int32(1) // one partition
	body.int32(id)
	body.int64(0)       // from the beginning
	body.int32(1 << 20) // partition max bytes

	require.NoError(t, socket.SetDeadline(time.Now().Add(10*time.Second)))

	const correlation = 9001

	_, err := socket.Write(request(apiFetch, fetchVersion, correlation, "conformance-consumer", body.buf))
	require.NoError(t, err)

	answer, err := readResponse(in, correlation)
	require.NoError(t, err)

	answer.int32() // throttle time

	var seen []consumed

	for range answer.arrayLen() {
		answer.text() // topic name

		for range answer.arrayLen() {
			partition := answer.int32()
			code := answer.int16()

			answer.int64() // high watermark
			answer.int64() // last stable offset

			for range answer.arrayLen() { // aborted transactions
				answer.int64()
				answer.int64()
			}

			size := answer.arrayLen()
			set := answer.buf[:size]
			answer.buf = answer.buf[size:]

			require.Zero(t, code, "the cluster answered error %d for partition %d", code, partition)

			seen = append(seen, readBatches(t, partition, set)...)
		}
	}

	require.NoError(t, answer.err)

	return seen
}

// readBatches decodes the format 2 record batches a fetch returns. The cluster
// has already checked the CRC of each one, so what arrives here is what the
// adapter wrote and what a real consumer would see.
func readBatches(t *testing.T, partition int32, set []byte) []consumed {
	t.Helper()

	var seen []consumed

	for len(set) > 12 {
		length := int(binary.BigEndian.Uint32(set[8:12]))

		end := 12 + length
		if end > len(set) {
			return seen // a batch truncated by the fetch limit, which is not an error
		}

		batch := set[12:end]
		set = set[end:]

		require.Equal(t, byte(magicRecordBatch), batch[4], "the cluster stored a batch in another format")

		count := int(binary.BigEndian.Uint32(batch[45:49]))
		records := batch[49:]

		for range count {
			one, rest, ok := readRecord(records)
			if !ok {
				return seen
			}

			one.partition = partition
			seen = append(seen, one)
			records = rest
		}
	}

	return seen
}

func readRecord(buf []byte) (consumed, []byte, bool) {
	size, read := binary.Varint(buf)
	if read <= 0 || int(size) > len(buf)-read {
		return consumed{}, nil, false
	}

	body := buf[read : read+int(size)]
	rest := buf[read+int(size):]

	body = body[1:] // attributes

	_, read = binary.Varint(body) // timestamp delta
	body = body[read:]

	_, read = binary.Varint(body) // offset delta
	body = body[read:]

	key, body, ok := readVarintBytes(body)
	if !ok {
		return consumed{}, nil, false
	}

	value, _, ok := readVarintBytes(body)
	if !ok {
		return consumed{}, nil, false
	}

	return consumed{key: string(key), value: string(value)}, rest, true
}

func readVarintBytes(buf []byte) ([]byte, []byte, bool) {
	size, read := binary.Varint(buf)
	if read <= 0 {
		return nil, nil, false
	}

	buf = buf[read:]

	if size < 0 {
		return nil, buf, true // absent, which is how a null key is spelled
	}

	if int(size) > len(buf) {
		return nil, nil, false
	}

	return buf[:size], buf[size:], true
}
