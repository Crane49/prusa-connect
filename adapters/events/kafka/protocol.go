package kafka

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"time"
)

// The API keys and versions this adapter uses. Both versions are older than
// the "flexible" ones (Metadata v9, Produce v9), which add tagged fields and
// compact lengths.
const (
	apiProduce  = 0
	apiMetadata = 3

	produceVersion  = 3
	metadataVersion = 4
)

// magicRecordBatch is record batch format 2. Every broker from 0.11 reads it,
// and it is the only format Kafka 4 accepts.
const magicRecordBatch = 2

// maxResponseBytes bounds a response read from a broker.
const maxResponseBytes = 32 << 20

// castagnoli is the CRC-32C table. A format 2 batch uses CRC-32C, not CRC-32.
//
//nolint:gochecknoglobals // an immutable lookup table the standard library asks to be built once.
var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// ErrProtocol reports a broker that answered with something this adapter cannot
// read.
var ErrProtocol = errors.New("kafka: the broker answered outside the protocol")

// The field widths, in bytes.
const (
	int16Width = 2
	int32Width = 4
	int64Width = 8
)

// encoder builds a request body. Every Kafka primitive is big-endian.
type encoder struct {
	buf []byte
}

// A signed field is written as the same bits in the unsigned type of its
// width.
//
//nolint:gosec // G115: int8 and byte are one byte wide, so the conversion drops nothing.
func (e *encoder) int8(v int8) { e.buf = append(e.buf, byte(v)) }

//nolint:gosec // G115: a reinterpretation at equal width, never a narrowing.
func (e *encoder) int16(v int16) { e.buf = binary.BigEndian.AppendUint16(e.buf, uint16(v)) }

//nolint:gosec // G115: a reinterpretation at equal width, never a narrowing.
func (e *encoder) int32(v int32) { e.buf = binary.BigEndian.AppendUint32(e.buf, uint32(v)) }

//nolint:gosec // G115: a reinterpretation at equal width, never a narrowing.
func (e *encoder) int64(v int64) { e.buf = binary.BigEndian.AppendUint64(e.buf, uint64(v)) }

func (e *encoder) boolean(v bool) { e.buf = append(e.buf, boolByte(v)) }
func (e *encoder) raw(v []byte)   { e.buf = append(e.buf, v...) }
func (e *encoder) varint(v int64) { e.buf = binary.AppendVarint(e.buf, v) }

// text writes a non-nullable string: a two-byte length and the bytes. A
// string too long for the length field is truncated.
func (e *encoder) text(v string) {
	if len(v) > math.MaxInt16 {
		v = v[:math.MaxInt16]
	}

	//nolint:gosec // G115: the truncation above holds the length to what the field carries.
	e.int16(int16(len(v)))
	e.buf = append(e.buf, v...)
}

// nullableText writes a string that may be absent. A length of -1 means
// absent.
func (e *encoder) nullableText(v string, present bool) {
	if !present {
		e.int16(-1)

		return
	}

	e.text(v)
}

// blob writes a byte array with its four-byte length.
//
//nolint:gosec // G115: the only blob written here is one record batch, whose size is one event body.
func (e *encoder) blob(v []byte) {
	e.int32(int32(len(v)))
	e.buf = append(e.buf, v...)
}

func boolByte(v bool) byte {
	if v {
		return 1
	}

	return 0
}

// decoder reads a response. A short read sets err, and every later read
// returns zero.
type decoder struct {
	buf []byte
	err error
}

func (d *decoder) int8() int8 {
	if !d.take(1) {
		return 0
	}

	//nolint:gosec // G115: byte and int8 are one byte wide, so the conversion drops nothing.
	v := int8(d.buf[0])
	d.buf = d.buf[1:]

	return v
}

//nolint:gosec // G115: a reinterpretation at equal width, reversing what the encoder wrote.
func (d *decoder) int16() int16 {
	if !d.take(int16Width) {
		return 0
	}

	v := int16(binary.BigEndian.Uint16(d.buf))
	d.buf = d.buf[int16Width:]

	return v
}

//nolint:gosec // G115: a reinterpretation at equal width, reversing what the encoder wrote.
func (d *decoder) int32() int32 {
	if !d.take(int32Width) {
		return 0
	}

	v := int32(binary.BigEndian.Uint32(d.buf))
	d.buf = d.buf[int32Width:]

	return v
}

// The adapter only skips int64 fields. The tests read the value.
//
//nolint:gosec,unparam // G115: a reinterpretation at equal width; the value is read by the consumer in the tests.
func (d *decoder) int64() int64 {
	if !d.take(int64Width) {
		return 0
	}

	v := int64(binary.BigEndian.Uint64(d.buf))
	d.buf = d.buf[int64Width:]

	return v
}

func (d *decoder) text() string {
	size := int(d.int16())
	if size < 0 {
		return "" // a null string, which reads the same as an empty one here
	}

	if !d.take(size) {
		return ""
	}

	v := string(d.buf[:size])
	d.buf = d.buf[size:]

	return v
}

// arrayLen reads an array length. A length larger than the bytes left is an
// error, so a corrupt length cannot cause a large allocation.
func (d *decoder) arrayLen() int {
	size := int(d.int32())
	if size < 0 {
		return 0
	}

	if size > len(d.buf) {
		d.fail(size)

		return 0
	}

	return size
}

// skipInt32Array steps over an array of node ids.
func (d *decoder) skipInt32Array() {
	for range d.arrayLen() {
		d.int32()
	}
}

func (d *decoder) take(n int) bool {
	if d.err != nil {
		return false
	}

	if len(d.buf) < n {
		d.fail(n)

		return false
	}

	return true
}

func (d *decoder) fail(want int) {
	if d.err == nil {
		d.err = fmt.Errorf("%w: %d more bytes were needed and %d were left", ErrProtocol, want, len(d.buf))
	}

	d.buf = nil
}

// request wraps a body in the Kafka request header and the length prefix.
func request(apiKey, version int16, correlation int32, clientID string, body []byte) []byte {
	header := &encoder{}
	header.int16(apiKey)
	header.int16(version)
	header.int32(correlation)
	header.nullableText(clientID, true)
	header.raw(body)

	framed := &encoder{}
	//nolint:gosec // G115: the request is one record batch plus a header, far below two gigabytes.
	framed.int32(int32(len(header.buf)))
	framed.raw(header.buf)

	return framed.buf
}

// readResponse reads one length-prefixed response and checks its correlation
// id. A mismatch means the connection is out of step and must be dropped.
func readResponse(r io.Reader, correlation int32) (*decoder, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, err
	}

	size := int(binary.BigEndian.Uint32(head[:]))
	if size < 4 || size > maxResponseBytes {
		return nil, fmt.Errorf("%w: the broker announced a %d byte response", ErrProtocol, size)
	}

	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}

	d := &decoder{buf: body}
	if got := d.int32(); got != correlation {
		return nil, fmt.Errorf("%w: the response answers request %d and %d was sent", ErrProtocol, got, correlation)
	}

	return d, nil
}

// recordBatch encodes one message as a format 2 batch of one record. One
// record per batch keeps the order of the event bus and needs no flush timer.
func recordBatch(key, value []byte, when time.Time) []byte {
	timestamp := when.UnixMilli()

	record := &encoder{}
	record.int8(0)   // attributes, unused per record
	record.varint(0) // timestamp delta from the batch's first
	record.varint(0) // offset delta, this being the first record
	appendVarintBytes(record, key)
	appendVarintBytes(record, value)
	record.varint(0) // header count

	framed := &encoder{}
	framed.varint(int64(len(record.buf)))
	framed.raw(record.buf)

	// The part of the batch the CRC covers.
	body := &encoder{}
	body.int16(0)         // attributes: no compression, create time
	body.int32(0)         // last offset delta, one record in
	body.int64(timestamp) // first timestamp
	body.int64(timestamp) // max timestamp
	body.int64(-1)        // producer id, this producer is not idempotent
	body.int16(-1)        // producer epoch
	body.int32(-1)        // base sequence
	body.int32(1)         // record count
	body.raw(framed.buf)

	// The batch length counts from the leader epoch to the end: the epoch, the
	// magic byte, the checksum and the body.
	length := int32Width + 1 + int32Width + len(body.buf)

	batch := &encoder{}
	batch.int64(0) // base offset, assigned by the broker
	//nolint:gosec // G115: the batch is one event body, far below two gigabytes.
	batch.int32(int32(length))
	batch.int32(-1) // partition leader epoch, unknown to a producer
	batch.int8(magicRecordBatch)
	//nolint:gosec // G115: a reinterpretation at equal width, which is how Kafka carries the checksum.
	batch.int32(int32(crc32.Checksum(body.buf, castagnoli)))
	batch.raw(body.buf)

	return batch.buf
}

// appendVarintBytes writes a record's key or value: a varint length and the
// bytes. A length of -1 means absent.
func appendVarintBytes(e *encoder, v []byte) {
	if v == nil {
		e.varint(-1)

		return
	}

	e.varint(int64(len(v)))
	e.raw(v)
}

// partitionFor picks the partition for a key, so every event about one
// printer is on one partition, in order. The hash is FNV-1a, not the Java
// client's murmur2, so another producer puts the same key elsewhere.
func partitionFor(key []byte, partitions []int32) int32 {
	if len(partitions) == 0 {
		return 0
	}

	const (
		offset32 = 2166136261
		prime32  = 16777619
	)

	hash := uint32(offset32)
	for _, b := range key {
		hash ^= uint32(b)
		hash *= prime32
	}

	//nolint:gosec // G115: the length is positive and small, and the empty case returned above.
	return partitions[hash%uint32(len(partitions))]
}
