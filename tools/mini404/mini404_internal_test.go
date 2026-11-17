package mini404

import (
	"slices"
	"strings"
	"testing"
)

func TestTheInvocationIsTheOneMINI404Expects(t *testing.T) {
	t.Parallel()

	cfg := Config{
		QEMUPath:     "/cache/qemu-system-buddy",
		Machine:      "prusa-mk4-034",
		FirmwarePath: "/cache/firmware.bin",
		EEPROMBank1:  "/run/eeprom-bank1.bin",
		EEPROMBank2:  "/run/eeprom-bank2.bin",
		XFlashPath:   "/run/xflash.bin",
		Dir:          "/run",
	}
	want := []string{
		"-machine", "prusa-mk4-034",
		"-kernel", "/cache/firmware.bin",
		"-display", "none",
		"-icount", "2",
		"-global", "STM32F4xx-usb.disable_sof_interrupt=true",
		"-netdev", "user,id=mini-eth,hostfwd=tcp:127.0.0.1:18080-:80",
		"-drive", "if=pflash,index=0,format=raw,file=/run/eeprom-bank1.bin",
		"-drive", "if=pflash,index=1,format=raw,file=/run/eeprom-bank2.bin",
		"-drive", "if=mtd,index=0,format=raw,file=/run/xflash.bin",
		"-chardev", "socket,id=p404-scriptcon,host=127.0.0.1,port=14000,server=on,wait=off",
		"-global", "p404-scriptcon.no_echo=on",
		"-qmp", "unix:/run/qmp.sock,server=on,wait=off",
	}

	got := qemuArgs(cfg, ports{http: 18080, scriptcon: 14000})
	if !slices.Equal(got, want) {
		t.Errorf("argv is\n%q\nand should be\n%q", got, want)
	}
}

func TestAUSBDirectoryAddsTheStickAndNothingElse(t *testing.T) {
	t.Parallel()

	cfg := Config{
		QEMUPath:     "qemu",
		Machine:      "prusa-mk4-034",
		FirmwarePath: "fw.bin",
		EEPROMBank1:  "b1",
		EEPROMBank2:  "b2",
		XFlashPath:   "x",
		Dir:          "/run",
		USBDir:       "/run/usb",
	}
	prt := ports{http: 1, scriptcon: 2}

	withStick := qemuArgs(cfg, prt)
	cfg.USBDir = ""
	without := qemuArgs(cfg, prt)

	if len(withStick) != len(without)+4 {
		t.Fatalf("the stick added %d arguments and should add 4", len(withStick)-len(without))
	}
	joined := strings.Join(withStick, " ")
	for _, want := range []string{
		"id=usbstick,format=raw,file=fat:rw:/run/usb",
		"usb-storage,drive=usbstick",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv is missing %q:\n%q", want, withStick)
		}
	}
	// The stick sits between the drives and the chardev, so the console flags
	// stay last where a reader looking for them expects them.
	if withStick[len(withStick)-2] != "-qmp" {
		t.Errorf("the qmp flag should be last and the argv ends %q", withStick[len(withStick)-2:])
	}
}

func TestTheInstructionCountIsTheOneTheCallerAsksFor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		set  int
		want string
	}{
		{name: "unset takes the default", set: 0, want: "2"},
		{name: "a puppy board asks for five", set: 5, want: "5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := qemuArgs(Config{Dir: "/run", ICount: tc.set}, ports{})
			i := slices.Index(args, "-icount")
			if i < 0 {
				t.Fatalf("argv carries no -icount: %q", args)
			}
			if args[i+1] != tc.want {
				t.Errorf("-icount is %q and should be %q", args[i+1], tc.want)
			}
		})
	}
}

func TestAConfigMissingAnArtefactIsRefusedByName(t *testing.T) {
	t.Parallel()

	full := Config{
		QEMUPath:     "qemu",
		Machine:      "prusa-mk4-034",
		FirmwarePath: "fw.bin",
		EEPROMBank1:  "b1",
		EEPROMBank2:  "b2",
		XFlashPath:   "x",
		Dir:          "/run",
	}
	if err := full.validate(); err != nil {
		t.Fatalf("a complete config was refused: %v", err)
	}

	for _, tc := range []struct {
		field string
		blank func(*Config)
	}{
		{"QEMUPath", func(c *Config) { c.QEMUPath = "" }},
		{"Machine", func(c *Config) { c.Machine = "" }},
		{"FirmwarePath", func(c *Config) { c.FirmwarePath = "" }},
		{"EEPROMBank1", func(c *Config) { c.EEPROMBank1 = "" }},
		{"EEPROMBank2", func(c *Config) { c.EEPROMBank2 = "" }},
		{"XFlashPath", func(c *Config) { c.XFlashPath = "" }},
		{"Dir", func(c *Config) { c.Dir = "" }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()

			cfg := full
			tc.blank(&cfg)
			err := cfg.validate()
			if err == nil {
				t.Fatalf("a config with no %s was accepted", tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("the error does not name %s: %v", tc.field, err)
			}
		})
	}
}

func TestARuntimeDirectoryTooDeepForAUnixSocketIsRefused(t *testing.T) {
	t.Parallel()

	deep := "/" + strings.Repeat("directory/", 12)
	cfg := Config{
		QEMUPath:     "qemu",
		Machine:      "m",
		FirmwarePath: "fw",
		EEPROMBank1:  "b1",
		EEPROMBank2:  "b2",
		XFlashPath:   "x",
		Dir:          deep,
	}

	err := cfg.validate()
	if err == nil {
		t.Fatal("a directory too deep for a unix socket was accepted")
	}
	if !strings.Contains(err.Error(), "Config.Dir") {
		t.Errorf("the error does not name the field: %v", err)
	}
}

func TestTheOutputTailKeepsTheEndOfTheStream(t *testing.T) {
	t.Parallel()

	w := &ringWriter{limit: 8}
	for _, chunk := range []string{"abcde", "fghij"} {
		n, err := w.Write([]byte(chunk))
		if err != nil {
			t.Fatalf("write %q: %v", chunk, err)
		}
		if n != len(chunk) {
			t.Fatalf("write reported %d bytes for %q", n, chunk)
		}
	}

	if got := w.String(); got != "cdefghij" {
		t.Errorf("the tail is %q and should be %q", got, "cdefghij")
	}
}

func TestTwoReservedPortsAreDifferentAndUsable(t *testing.T) {
	t.Parallel()

	prt, err := reservePorts(t.Context())
	if err != nil {
		t.Fatalf("reserve ports: %v", err)
	}
	if prt.http == 0 || prt.scriptcon == 0 {
		t.Fatalf("a reserved port is zero: %+v", prt)
	}
	if prt.http == prt.scriptcon {
		t.Errorf("both ports are %d, so the chardev would collide with the forward", prt.http)
	}
}
