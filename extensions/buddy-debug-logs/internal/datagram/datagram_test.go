package datagram_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/prusa3d/buddy-debug-logs/internal/datagram"
)

// The datagrams below are built from the firmware's own format strings, quoted
// in the package comment, with the values a real MK4 sends.

func TestASyslogLineIsRead(t *testing.T) {
	t.Parallel()

	raw := `<11>1 - 10:9c:70:2a:bb:cd buddy Connect - [metrics@64164 v="5" msg="42" tm="123456789"] ` +
		`Connect error: connection reset`

	got, err := datagram.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := datagram.Datagram{
		Kind:      datagram.KindSyslog,
		MAC:       "10:9c:70:2a:bb:cd",
		Seq:       42,
		TmUS:      123456789,
		Severity:  datagram.SeverityError,
		Component: "Connect",
		Text:      "Connect error: connection reset",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// Firmware 6.4.0 writes no structured data, so a line has no sequence and
// no time since boot (v6.4.0:src/logging/log_dest_syslog.cpp:
// "<%i>1 - %s %s %s - - %s"). The line is one an XL on 6.4.0 sent.
func TestASyslogLineWithoutStructuredDataIsRead(t *testing.T) {
	t.Parallel()

	raw := `<14>1 - 10:9c:70:26:7a:00 buddy Dwarf_4 - - 26305.446s [INFO  - ModbusControl:0] ` +
		`Set Heatbreak requested temperature: 36`

	got, err := datagram.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := datagram.Datagram{
		Kind:      datagram.KindSyslog,
		MAC:       "10:9c:70:26:7a:00",
		Severity:  datagram.SeverityInfo,
		Component: "Dwarf_4",
		Text:      "26305.446s [INFO  - ModbusControl:0] Set Heatbreak requested temperature: 36",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// The firmware maps its levels to PRI = 8 + severity, facility 1.
func TestTheSeverityComesFromThePriority(t *testing.T) {
	t.Parallel()

	for pri, want := range map[string]datagram.Severity{
		"10": datagram.SeverityCritical, "11": datagram.SeverityError,
		"12": datagram.SeverityWarning, "14": datagram.SeverityInfo,
	} {
		raw := "<" + pri + `>1 - aa:bb:cc:dd:ee:ff buddy GUI - [metrics@64164 v="5" msg="1" tm="1"] x`

		got, err := datagram.Parse([]byte(raw))
		if err != nil || got.Severity != want {
			t.Errorf("<%s> reads as %v (%v), want %v", pri, got.Severity, err, want)
		}
	}
}

// A MAC in upper case is read in lower case, the spelling the state tree uses.
func TestTheMACIsReadInLowerCase(t *testing.T) {
	t.Parallel()

	raw := `<14>1 - 10:9C:70:2A:BB:CD buddy GUI - [metrics@64164 v="5" msg="1" tm="1"] x`

	got, err := datagram.Parse([]byte(raw))
	if err != nil || got.MAC != "10:9c:70:2a:bb:cd" {
		t.Errorf("MAC = %q (%v)", got.MAC, err)
	}
}

func TestAMetricsDatagramIsRead(t *testing.T) {
	t.Parallel()

	raw := "<14>1 - 10:9c:70:2a:bb:cd buddy - - - msg=7,tm=5000000,v=4 " +
		"temp_bed v=60.120003 -1520\n" +
		"fan_speed v=4200i 830\n" +
		"print_filename v=\"benchy \\\"v2\\\".gcode\" 12\n" +
		"power_panic v=T 0\n" +
		"temp_noz,n=0,a=1 value=215.30 410\n" +
		"heap error=\"value too long\" 7\n"

	got, err := datagram.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got.Kind != datagram.KindMetrics || got.Seq != 7 || got.TmUS != 5000000 || got.MAC != "10:9c:70:2a:bb:cd" {
		t.Fatalf("header = %+v", got)
	}

	want := []datagram.Point{
		{Line: "temp_bed v=60.120003 -1520", Name: "temp_bed", Fields: map[string]any{"v": 60.120003}, OffsetUS: -1520},
		{Line: "fan_speed v=4200i 830", Name: "fan_speed", Fields: map[string]any{"v": int64(4200)}, OffsetUS: 830},
		{
			Line: `print_filename v="benchy \"v2\".gcode" 12`, Name: "print_filename",
			Fields: map[string]any{"v": `benchy "v2".gcode`}, OffsetUS: 12,
		},
		{Line: "power_panic v=T 0", Name: "power_panic", Fields: map[string]any{"v": true}},
		{
			Line: "temp_noz,n=0,a=1 value=215.30 410", Name: "temp_noz",
			Tags: map[string]string{"n": "0", "a": "1"}, Fields: map[string]any{"value": 215.30}, OffsetUS: 410,
		},
		{
			Line:     `heap error="value too long" 7`,
			Name:     "heap",
			Fields:   map[string]any{"error": "value too long"},
			OffsetUS: 7,
		},
	}

	if !reflect.DeepEqual(got.Points, want) {
		t.Errorf("points\n got  %+v\n want %+v", got.Points, want)
	}
}

// A value that is not a finite number is kept as its text, so the point can
// be written as JSON and a failed sensor's nan is not lost.
func TestANumberThatIsNotFiniteIsKeptAsText(t *testing.T) {
	t.Parallel()

	point := datagram.ParsePoint("temp_bed v=nan,a=inf,b=-Inf,c=NaN,d=1.5 0")

	want := map[string]any{"v": "nan", "a": "inf", "b": "-Inf", "c": "NaN", "d": 1.5}
	if !reflect.DeepEqual(point.Fields, want) {
		t.Fatalf("fields = %#v, want %#v", point.Fields, want)
	}

	if _, err := json.Marshal(point.Fields); err != nil {
		t.Errorf("the fields cannot be written as JSON: %v", err)
	}
}

// A custom metric writes its own fields, several of them.
func TestACustomMetricKeepsEveryField(t *testing.T) {
	t.Parallel()

	point := datagram.ParsePoint("fan,fan=print state=1,pwm=100i,measured=4200i 830")

	if point.Tags["fan"] != "print" || point.Fields["pwm"] != int64(100) || point.Fields["state"] != 1.0 {
		t.Errorf("point = %+v", point)
	}
}

func TestADatagramInAnotherFormatIsRefused(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"empty":               "",
		"plain text":          "hello",
		"no version":          "<14>2 - a b c d e",
		"too few fields":      "<14>1 - mac buddy",
		"a priority too high": "<999>1 - a b c d - x",
		"too large":           strings.Repeat("x", datagram.MaxBytes+1),
	} {
		if _, err := datagram.Parse([]byte(raw)); !errors.Is(err, datagram.ErrUnreadable) {
			t.Errorf("%s: err = %v, want ErrUnreadable", name, err)
		}
	}
}

func TestASeverityIsNamedAndReadBack(t *testing.T) {
	t.Parallel()

	for s := datagram.SeverityEmergency; s <= datagram.SeverityDebug; s++ {
		back, ok := datagram.ParseSeverity(s.String())
		if !ok || back != s {
			t.Errorf("%v read back as %v, %v", s, back, ok)
		}
	}

	if _, ok := datagram.ParseSeverity("loud"); ok {
		t.Error("an unknown word was read as a severity")
	}
}

// A point whose series is only separators once made the parser index an
// empty slice. It is kept by its line, and the datagram is read.
func TestAPointWithAnEmptySeriesIsKeptByItsLine(t *testing.T) {
	t.Parallel()

	got, err := datagram.Parse([]byte("<0>1 0 aa:bb:cc:dd:ee:ff 0 0 0 - msg= , 0"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(got.Points) != 1 || got.Points[0].Name != ", 0" || len(got.Points[0].Fields) != 0 {
		t.Errorf("points = %+v", got.Points)
	}
}

// The firmware writes the MAC as six lower-case pairs joined by colons. Any
// other HOSTNAME is refused, so a sender cannot fill the sources with names.
func TestAHostnameThatIsNotAMACIsRefused(t *testing.T) {
	t.Parallel()

	for _, hostname := range []string{
		"printer", "10-9c-70-2a-bb-cd", "109c702abbcd", "10:9c:70:2a:bb", "10:9c:70:2a:bb:cd:ee",
		"10:9c:70:2a:bb:cg", "10:9c:70:2a:bbbcd",
	} {
		raw := "<14>1 - " + hostname + ` buddy GUI - [metrics@64164 v="5" msg="1" tm="1"] x`

		if _, err := datagram.Parse([]byte(raw)); !errors.Is(err, datagram.ErrUnreadable) {
			t.Errorf("%s: err = %v, want ErrUnreadable", hostname, err)
		}
	}
}

// A datagram larger than any the firmware sends is refused.
func TestADatagramOverTheFirmwareSizeIsRefused(t *testing.T) {
	t.Parallel()

	head := `<14>1 - aa:bb:cc:dd:ee:ff buddy GUI - [metrics@64164 v="5" msg="1" tm="1"] `
	raw := head + strings.Repeat("x", datagram.MaxBytes-len(head)+1)

	if _, err := datagram.Parse([]byte(raw)); !errors.Is(err, datagram.ErrUnreadable) {
		t.Errorf("a datagram of %d bytes: err = %v, want ErrUnreadable", len(raw), err)
	}

	if _, err := datagram.Parse([]byte(raw[:datagram.MaxBytes])); err != nil {
		t.Errorf("a datagram of %d bytes was refused: %v", datagram.MaxBytes, err)
	}
}

// Too many points, or a point with too many fields, refuses the datagram.
func TestAMetricsDatagramOverTheBoundsIsRefused(t *testing.T) {
	t.Parallel()

	head := "<14>1 - aa:bb:cc:dd:ee:ff buddy - - - msg=1,tm=1,v=4 "

	points := strings.Repeat("a v=1 0\n", datagram.MaxPoints+1)
	if _, err := datagram.Parse([]byte(head + points)); !errors.Is(err, datagram.ErrUnreadable) {
		t.Errorf("%d points: err = %v, want ErrUnreadable", datagram.MaxPoints+1, err)
	}

	got, err := datagram.Parse([]byte(head + strings.Repeat("a v=1 0\n", datagram.MaxPoints)))
	if err != nil || len(got.Points) != datagram.MaxPoints {
		t.Errorf("%d points read as %d, %v", datagram.MaxPoints, len(got.Points), err)
	}

	fields := make([]string, datagram.MaxFields+1)
	for i := range fields {
		fields[i] = "f" + strings.Repeat("x", i) + "=1"
	}

	if _, err = datagram.Parse([]byte(head + "a " + strings.Join(fields, ",") + " 0\n")); !errors.Is(
		err, datagram.ErrUnreadable,
	) {
		t.Errorf("%d fields: err = %v, want ErrUnreadable", len(fields), err)
	}
}

// A control character in a line would forge a second line in a download or
// move a terminal's cursor. Each one is escaped.
func TestAControlCharacterIsEscaped(t *testing.T) {
	t.Parallel()

	raw := "<14>1 - aa:bb:cc:dd:ee:ff buddy GU\x1bI - - one\nfake 2026 line\x1b[31m red \u009b2J\x7f \xff end"

	got, err := datagram.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := `one\x0afake 2026 line\x1b[31m red \x9b2J\x7f \xff end`
	if got.Text != want || got.Component != `GU\x1bI` {
		t.Errorf("text = %q, component = %q, want %q", got.Text, got.Component, want)
	}

	point := datagram.ParsePoint("temp\x1b v=1\r 0")
	if point.Line != `temp\x1b v=1\x0d 0` || point.Name != `temp\x1b` {
		t.Errorf("point = %+v", point)
	}
}

// Text longer than MaxTextBytes is cut, and never inside a character.
func TestALongTextIsCut(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", datagram.MaxTextBytes*3/5)

	got, err := datagram.Parse([]byte("<14>1 - aa:bb:cc:dd:ee:ff buddy GUI - - x" + long))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(got.Text) > datagram.MaxTextBytes || !utf8.ValidString(got.Text) {
		t.Errorf("text of %d bytes, valid UTF-8 %v", len(got.Text), utf8.ValidString(got.Text))
	}
}

// FuzzParse holds Parse to its bounds on any input: it never panics, and what
// it reads carries a valid MAC, no control character and no more than the
// bounded points, fields and text.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		`<11>1 - 10:9c:70:2a:bb:cd buddy Connect - [metrics@64164 v="5" msg="42" tm="123456789"] reset`,
		`<14>1 - 10:9c:70:26:7a:00 buddy Dwarf_4 - - 26305.446s [INFO  - ModbusControl:0] temperature: 36`,
		"<14>1 - 10:9c:70:2a:bb:cd buddy - - - msg=7,tm=5000000,v=4 temp_bed v=60.120003 -1520\n" +
			"fan_speed v=4200i 830\nprint_filename v=\"benchy \\\"v2\\\".gcode\" 12\ntemp_noz,n=0,a=1 value=215.30 410\n",
		"<0>1 0 0 0 0 0 - msg= , 0",
		"<0>1 0 aa:bb:cc:dd:ee:ff 0 0 0 - msg= , 0",
		"<0>1 0 aa:bb:cc:dd:ee:ff 0 0 0 - msg= ,,, \"\" 0",
		`<0>1 - aa:bb:cc:dd:ee:ff a b - [x="]"] [y] z`,
		"<14>1 - aa:bb:cc:dd:ee:ff buddy GUI - - \x1b[2J\r\nforged",
		"<0>1 0 00:00:00:00:00:00 0 " + strings.Repeat("0", 1500) + " 0 -",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		got, err := datagram.Parse(raw)
		if err != nil {
			return
		}

		if !datagram.ValidMAC(got.MAC) {
			t.Errorf("read a MAC %q", got.MAC)
		}

		if len(got.Points) > datagram.MaxPoints {
			t.Errorf("read %d points", len(got.Points))
		}

		for _, text := range []string{got.Text, got.Component} {
			if datagram.Clean(text) != text || len(text) > datagram.MaxTextBytes {
				t.Errorf("read an unclean or long text %q", text)
			}
		}

		for _, point := range got.Points {
			if len(point.Fields) > datagram.MaxFields || len(point.Tags) > datagram.MaxFields {
				t.Errorf("read a point with %d fields and %d tags", len(point.Fields), len(point.Tags))
			}

			if datagram.Clean(point.Line) != point.Line || datagram.Clean(point.Name) != point.Name ||
				len(point.Line) > datagram.MaxTextBytes {
				t.Errorf("read an unclean or long point %q", point.Line)
			}
		}
	})
}
