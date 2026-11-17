package mini404

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
)

// The bank format, the item id hash and the transaction framing are a port of
// the vendor generator at utils/persistent_stores/eeprom.py in
// Prusa-Firmware-Buddy, used the way the vendor's own integration harness uses
// it in tests/integration/conftest.py. The geometry constants below are the
// same numbers the firmware compiles in at
// src/persistent_stores/store_instances/config_store/backend_instance.hpp.
//
// TestBanksMatchTheVendorGenerator is the drift alarm. It compares this code
// against bank images produced by that python, so a firmware bump that moves
// the format fails a test instead of producing a printer that boots into the
// first-run wizard.
const (
	// eepromFileSize is the size of one pflash drive MINI404 mounts, the
	// same device size EEPROMBankSize names for launcher callers. The
	// vendor harness creates both bank files at this size before writing.
	eepromFileSize = EEPROMBankSize

	// eepromFirstBankOffset, eepromUsableSize and eepromBankSize are
	// start_address and BANK_SIZE from backend_instance.hpp. The 8096 is
	// the firmware's own literal, not a rounding of 8192, so it is
	// reproduced as written.
	eepromFirstBankOffset = 0x500
	eepromUsableSize      = 8096
	eepromBankSize        = (eepromUsableSize - eepromFirstBankOffset) / 2

	// eepromMaxItemSize is journal::Backend::MAX_ITEM_SIZE. An item at or
	// over it does not compile in the firmware.
	eepromMaxItemSize = 512

	// selftestResultSize is sizeof(SelftestResult) in the firmware, the
	// width of the Selftest Result Gears record the vendor harness fills.
	selftestResultSize = 21

	// eepromIDMask keeps the 14 bits of a journal item id, and the three
	// header masks below pack the last-item flag, the id and the 9 bit
	// length into the layout eepromItemHeader documents.
	eepromIDMask       = 0x3FFF
	eepromLowSevenBits = 0x7F
	eepromIDLowWidth   = 7
	eepromLowBit       = 0x01
	eepromLowByte      = 0xFF

	// eepromSequenceID and eepromVersion are what the vendor harness passes.
	// The version is journal::Backend::CURRENT_VERSION.
	eepromSequenceID = 1
	eepromVersion    = 1

	// connectHostFieldSize and connectTokenFieldSize are
	// sizeof(std::array<char, connect_host_size + 1>) and its token twin from
	// constants.hpp. The journal terminates the firmware if a record's length
	// is not exactly sizeof of the item's type, so these are exact.
	connectHostFieldSize  = 21
	connectTokenFieldSize = 21
)

// ErrHostTooLong reports a seed host that does not fit the firmware's fixed
// field with its terminator.
var ErrHostTooLong = errors.New("connect host too long")

// ErrTokenTooLong reports a seed token that does not fit the firmware's fixed
// field with its terminator.
var ErrTokenTooLong = errors.New("connect token too long")

// ConnectSeed is the Connect client configuration written into the EEPROM so
// firmware dials a local server without anybody touching the printer's screen.
//
// An empty Token omits the Connect Token journal item, leaving the firmware on
// its own default. Firmware with no token registers itself over the /p/register
// flow; firmware with one dials /p/ws directly.
type ConnectSeed struct {
	Host    string
	Port    uint16
	TLS     bool
	Token   string
	Enabled bool
}

func (s ConnectSeed) validate() error {
	if len(s.Host) >= connectHostFieldSize {
		return fmt.Errorf("%w: %d bytes, the field holds %d with its terminator",
			ErrHostTooLong, len(s.Host), connectHostFieldSize-1)
	}

	if len(s.Token) >= connectTokenFieldSize {
		return fmt.Errorf("%w: %d bytes, the field holds %d with its terminator",
			ErrTokenTooLong, len(s.Token), connectTokenFieldSize-1)
	}

	return nil
}

// eepromItem is one journal record: the item name the firmware hashes into an
// id, and the exact bytes of its C++ type.
type eepromItem struct {
	name string
	data []byte
}

// wizardSkipItems is DEFAULT_EEPROM_CONTENT from the vendor harness. These are
// the items that keep a fresh printer out of selftest, calibration and the
// first-layer wizard, which is what makes a headless boot reach the network at
// all. The gears value is selftestResultSize bytes of the vendor filler.
func wizardSkipItems() []eepromItem {
	gears := make([]byte, selftestResultSize)
	for i := range gears {
		gears[i] = 0xAA
	}

	return []eepromItem{
		{name: "Run Selftest", data: []byte{0}},
		{name: "Run XYZ Calibration", data: []byte{0}},
		{name: "Run First Layer", data: []byte{0}},
		{name: "FSensor Enabled", data: []byte{0}},
		{name: "Selftest Result Gears", data: gears},
	}
}

// connectItems returns the Connect records for the seed. The order is the
// order the golden fixture was generated in, and the golden test holds it.
func connectItems(seed ConnectSeed) []eepromItem {
	items := []eepromItem{
		{name: "Connect Host", data: eepromFixedString(seed.Host, connectHostFieldSize)},
	}

	if seed.Token != "" {
		items = append(items, eepromItem{
			name: "Connect Token",
			data: eepromFixedString(seed.Token, connectTokenFieldSize),
		})
	}

	return append(items,
		eepromItem{name: "Connect Port", data: binary.LittleEndian.AppendUint16(nil, seed.Port)},
		eepromItem{name: "Connect TLS", data: []byte{eepromBoolByte(seed.TLS)}},
		eepromItem{name: "Connect Enabled", data: []byte{eepromBoolByte(seed.Enabled)}},
	)
}

// WriteEEPROMBanks writes the two EEPROM pflash images MINI404 mounts into dir
// and returns their paths. The first bank carries one journal transaction with
// the wizard-skip items and the seed; the second is erased, which is the state
// the vendor harness leaves it in.
//
// The output depends on nothing but the seed. There is no timestamp, no
// padding of unwritten space with anything but the erase value, and no
// randomness anywhere in the format, so two calls with one seed are identical
// files.
func WriteEEPROMBanks(dir string, seed ConnectSeed) (bank1, bank2 string, err error) {
	image, err := eepromImage(seed)
	if err != nil {
		return "", "", err
	}

	erased := make([]byte, eepromFileSize)
	for i := range erased {
		erased[i] = 0xFF
	}

	bank1 = filepath.Join(dir, "bank1.bin")
	bank2 = filepath.Join(dir, "bank2.bin")

	if err = os.WriteFile(bank1, image, 0o600); err != nil {
		return "", "", fmt.Errorf("write eeprom bank 1: %w", err)
	}

	if err = os.WriteFile(bank2, erased, 0o600); err != nil {
		return "", "", fmt.Errorf("write eeprom bank 2: %w", err)
	}

	return bank1, bank2, nil
}

// eepromImage builds one pflash image: erased up to the first bank, one
// transaction in that bank, erased from there to the end of the drive.
func eepromImage(seed ConnectSeed) ([]byte, error) {
	if err := seed.validate(); err != nil {
		return nil, err
	}

	items := append(wizardSkipItems(), connectItems(seed)...)

	transaction, err := eepromTransaction(items)
	if err != nil {
		return nil, err
	}

	bank := append(eepromBankHeader(eepromSequenceID, eepromVersion), transaction...)
	if len(bank) > eepromBankSize {
		return nil, fmt.Errorf("eeprom transaction is %d bytes, the bank holds %d", len(bank), eepromBankSize)
	}

	image := make([]byte, eepromFileSize)
	for i := range image {
		image[i] = 0xFF
	}

	copy(image[eepromFirstBankOffset:], bank)

	return image, nil
}

// eepromBankHeader is a 32 bit sequence id and a 16 bit version, followed by
// their crc.
func eepromBankHeader(sequenceID uint32, version uint16) []byte {
	header := binary.LittleEndian.AppendUint32(nil, sequenceID)
	header = binary.LittleEndian.AppendUint16(header, version)

	return binary.LittleEndian.AppendUint32(header, crc32.ChecksumIEEE(header))
}

// eepromTransaction frames the items and appends the crc over all of them. The
// last item carries the LastItem flag, which is how the firmware finds the end.
func eepromTransaction(items []eepromItem) ([]byte, error) {
	var data []byte

	for i, item := range items {
		if len(item.data) >= eepromMaxItemSize {
			return nil, fmt.Errorf("eeprom item %q is %d bytes, the limit is %d",
				item.name, len(item.data), eepromMaxItemSize-1)
		}

		dataLen := uint16(len(item.data)) //nolint:gosec // bounded by the eepromMaxItemSize check above
		data = append(data, eepromItemHeader(i == len(items)-1, eepromItemID(item.name), dataLen)...)
		data = append(data, item.data...)
	}

	return binary.LittleEndian.AppendUint32(data, crc32.ChecksumIEEE(data)), nil
}

// eepromItemID is journal::hash: the two most significant bytes of the sha256
// of the item name, masked to 14 bits.
func eepromItemID(name string) uint16 {
	sum := sha256.Sum256([]byte(name))

	return (uint16(sum[0])<<8 | uint16(sum[1])) & eepromIDMask
}

// eepromItemHeader packs the last-item flag, the 14 bit id and the 9 bit
// length into three bytes:
// [last item][7 lsb of id] -- [7 msb of id][1 lsb of len] -- [8 msb of len].
func eepromItemHeader(lastItem bool, id, dataLen uint16) []byte {
	first := byte(id&eepromLowSevenBits)<<1 | eepromBoolByte(lastItem)
	second := byte(dataLen&eepromLowBit)<<eepromIDLowWidth | byte((id>>eepromIDLowWidth)&eepromLowSevenBits)
	third := byte((dataLen >> 1) & eepromLowByte)

	return []byte{first, second, third}
}

// fixedString renders a Go string as a C++ std::array<char, size>: the bytes,
// a terminator, and zeroes to the end of the field. The caller has already
// checked that it fits.
func eepromFixedString(value string, size int) []byte {
	field := make([]byte, size)
	copy(field, value)

	return field
}

func eepromBoolByte(value bool) byte {
	if value {
		return 1
	}

	return 0
}
