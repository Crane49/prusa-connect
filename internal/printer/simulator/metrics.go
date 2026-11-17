package simulator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// What a Buddy printer does when Metrics & Log is on: it sends its log lines
// and its metrics over UDP to the host M334 named, in the formats of
// Prusa-Firmware-Buddy v6.10.1. M334 asks a person on the display first, as
// the firmware does (M330.cpp), and here the display is the dialog a host
// answers with DIALOG_ACTION.

// metricsDialogCode is the firmware's code for the M334 warning, XX403 with a
// printer prefix (buddy-error-codes.yaml).
const metricsDialogCode = "13403"

// syslogMaxBytes is where the firmware cuts a syslog datagram
// (log_dest_syslog.cpp: static char buffer[200]).
const syslogMaxBytes = 200

// metricsTick is how often this printer sends a datagram of metrics. The
// firmware sends when its 1024-byte buffer fills or a second has passed.
const metricsTick = time.Second

// logTick is how often this printer writes a routine log line.
const logTick = 5 * time.Second

// The ports the firmware sends to before an M334 names others.
const (
	defaultMetricsPort = 8514
	defaultSyslogPort  = 13514
)

// syslogFacilityUser is facility 1, user, shifted into a syslog priority.
const syslogFacilityUser = 8

// The fan the simulator reports while it prints, and its PWM as a share.
const (
	printingFanRPM = 4200
	fanPWMDivisor  = 20
)

// idleCPUUsage is the CPU load in percent the simulator reports.
const idleCPUUsage = 30

// wobblePeriod is the period, in milliseconds, of the wobble on the
// temperatures.
const wobblePeriod = 1000

// wobbleCentre moves the wobble to either side of zero, and cpuWobble scales
// it to whole percent of CPU load.
const (
	wobbleCentre = 0.5
	cpuWobble    = 10
)

// sendTimeout bounds how long sending one datagram may take.
const sendTimeout = time.Second

// metricsTarget is the Metrics & Log setting the firmware stores: whether it
// is on, and where it sends. Switching it off keeps the host and the ports,
// as the firmware's config store does. A port of 0 sends nothing on that
// stream (syslog_transport.cpp: remote_port == 0).
type metricsTarget struct {
	on          bool
	host        string
	metricsPort int
	syslogPort  int
}

// MetricsTarget reports where the printer sends its metrics and log lines,
// and false while Metrics & Log is off.
func (p *Printer) MetricsTarget() (host string, metricsPort, syslogPort int, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.metrics.on {
		return "", 0, 0, false
	}

	return p.metrics.host, p.metrics.metricsPort, p.metrics.syslogPort, true
}

// MAC is the Ethernet address the printer names itself by, in the lower case
// its syslog writes.
func (p *Printer) MAC() string { return p.cfg.MAC }

// macFor derives a stable address from the fingerprint, in Prusa's own
// prefix, so the same simulated printer keeps its MAC across runs.
func macFor(fingerprint string) string {
	sum := sha256.Sum256([]byte(fingerprint))

	return fmt.Sprintf("10:9c:70:%02x:%02x:%02x", sum[0], sum[1], sum[2])
}

// noteMetricsConfig reads an M334 line and, when it would change anything,
// raises the warning the firmware raises. The change is applied only when a
// person answers Yes. It reports whether the line was an M334.
func (p *Printer) noteMetricsConfig(line []byte) bool {
	fields := strings.Fields(string(line))
	if len(fields) == 0 || !strings.EqualFold(fields[0], "M334") {
		return false
	}

	p.mu.Lock()
	next := parseM334(fields[1:], p.metrics)
	unchanged := next == p.metrics
	p.nextDialogID++
	id := p.nextDialogID
	p.mu.Unlock()

	if unchanged {
		return true
	}

	p.raiseAnswered(Dialog{
		ID:      id,
		Code:    metricsDialogCode,
		Title:   "G-CODE METRICS CONFIG CHANGE",
		Text:    "Gcode is trying to change metrics configuration.\n\nAllow the changes?",
		Buttons: []string{"Yes", "No"},
	}, func(button string) {
		if button != "Yes" {
			return
		}

		// One character is not a host, and the firmware switches the
		// feature off for it (metrics_reconfigure).
		if len(next.host) < minMetricsHost {
			next.on = false
		}

		p.mu.Lock()
		p.metrics = next
		p.mu.Unlock()

		p.logLine(syslogInfo, "Metrics", "metrics and syslog reconfigured")
	})

	return true
}

// parseM334 reads `M334 [host] [metrics_port] [syslog_port]` the way the
// firmware's sscanf("%20s %i %i") does, over the setting it has stored. A bare
// M334 switches Metrics & Log off and keeps the host and the ports. A field
// that does not read ends the scan, and what follows it keeps its stored
// value (M330.cpp).
func parseM334(args []string, stored metricsTarget) metricsTarget {
	next := stored
	next.on = len(args) > 0

	if !next.on {
		return next
	}

	next.host = args[0]
	ports := args[1:]

	if len(next.host) > maxMetricsHost {
		// %20s stops after twenty characters, and the first %i reads on from
		// there: the rest of the word comes before the ports.
		ports = append([]string{next.host[maxMetricsHost:]}, ports...)
		next.host = next.host[:maxMetricsHost]
	}

	for i, port := range []*int{&next.metricsPort, &next.syslogPort} {
		if len(ports) <= i {
			break
		}

		value, err := strconv.Atoi(ports[i])
		if err != nil || value < 0 || value > 65535 {
			break
		}

		*port = value
	}

	return next
}

// The shortest and the longest host the firmware keeps Metrics & Log on for
// (metrics_reconfigure, config_store_ns::metrics_host_size).
const (
	minMetricsHost = 2
	maxMetricsHost = 20
)

// Syslog severities, as the firmware maps its own (log_dest_syslog.cpp).
const (
	syslogError   = 3
	syslogWarning = 4
	syslogInfo    = 6
)

// logLine sends one syslog datagram when the syslog stream is on.
func (p *Printer) logLine(severity int, component, text string) {
	p.mu.Lock()
	target := p.metrics
	p.syslogSeq++
	seq := p.syslogSeq
	p.mu.Unlock()

	if !target.on || target.syslogPort == 0 {
		return
	}

	line := fmt.Sprintf(`<%d>1 - %s buddy %s - [metrics@64164 v="5" msg="%d" tm="%d"] %s`,
		syslogFacilityUser+severity, p.cfg.MAC, component, seq, p.sinceBoot(), text)
	if len(line) > syslogMaxBytes {
		line = line[:syslogMaxBytes]
	}

	sendUDP(target.host, target.syslogPort, line)
}

// sinceBoot is the firmware's tm: microseconds since the printer started.
func (p *Printer) sinceBoot() int64 { return time.Since(p.booted).Microseconds() }

// emit sends metrics and routine log lines while Metrics & Log is on, until
// ctx ends. It costs nothing while the feature is off. Each Run starts one,
// and the token in p.emitter lets one send at a time: a later Run's emitter
// waits until the one before it has ended.
func (p *Printer) emit(ctx context.Context) {
	select {
	case p.emitter <- struct{}{}:
	case <-ctx.Done():
		return
	}

	defer func() { <-p.emitter }()

	metrics := time.NewTicker(metricsTick)
	defer metrics.Stop()

	logs := time.NewTicker(logTick)
	defer logs.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-metrics.C:
			p.sendMetrics()
		case <-logs.C:
			p.routineLog()
		}
	}
}

// sendMetrics sends one datagram: the header, then one point per line, each
// with its offset from the header in microseconds (metric_handlers.cpp).
func (p *Printer) sendMetrics() {
	p.mu.Lock()
	target := p.metrics
	printing := p.job.path != ""
	file := p.job.path
	p.metricsSeq++
	seq := p.metricsSeq
	p.mu.Unlock()

	if !target.on || target.metricsPort == 0 {
		return
	}

	nozzle, bed, fan := ambientTemp, ambientTemp, 0
	if printing {
		nozzle, bed, fan = printingNozzleTemp, printingBedTemp, printingFanRPM
	}

	// A small wobble, so a chart drawn from it is not a flat line.
	wobble := float64(time.Now().UnixMilli()%wobblePeriod)/wobblePeriod - wobbleCentre

	var b strings.Builder

	fmt.Fprintf(&b, "<14>1 - %s buddy - - - msg=%d,tm=%d,v=4 ", p.cfg.MAC, seq, p.sinceBoot())
	fmt.Fprintf(&b, "temp_noz,n=0,a=1 value=%.2f -120\n", nozzle+wobble)
	fmt.Fprintf(&b, "temp_bed v=%f -80\n", bed+wobble/2)
	fmt.Fprintf(&b, "fan,fan=print state=%d,pwm=%d,measured=%d -40\n", boolInt(printing), fan/fanPWMDivisor, fan)
	fmt.Fprintf(&b, "cpu_usage v=%di 0\n", idleCPUUsage+int(wobble*cpuWobble))
	fmt.Fprintf(&b, "is_printing v=%di 0\n", boolInt(printing))

	if printing {
		fmt.Fprintf(&b, "print_filename v=%q 0\n", file)
	}

	sendUDP(target.host, target.metricsPort, b.String())
}

// routineLog writes the kind of line a printer logs while it runs.
func (p *Printer) routineLog() {
	p.mu.Lock()
	state := p.state
	p.mu.Unlock()

	p.logLine(syslogInfo, "Connect", "telemetry sent, state "+state)

	if state == stateError {
		p.logLine(syslogError, "Marlin", "printer is in an error state")
	}
}

func boolInt(on bool) int {
	if on {
		return 1
	}

	return 0
}

// sendUDP sends one datagram and drops it on any failure, as the firmware's
// transport does: nothing answers a UDP datagram, and a lost one is lost.
func sendUDP(host string, port int, payload string) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return
	}

	_, _ = conn.Write([]byte(payload)) // a dropped datagram is what UDP is
	_ = conn.Close()                   // a UDP socket holds nothing to flush
}

// firstDialogID is the first id the simulator gives a dialog of its own,
// above the ids a test or a demo raises by hand.
const firstDialogID = 1000
