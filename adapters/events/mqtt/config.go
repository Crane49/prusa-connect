package mqtt

import (
	"fmt"
	"strconv"
	"sync/atomic"
	"time"
)

// lastClientStamp is the number in the last generated client id.
var lastClientStamp atomic.Int64

// generatedClientID returns "prusa-server-<nanoseconds>". The number is always
// above the last one returned, because a coarse clock can give two sinks the
// same time, and a broker drops the older of two connections with one id.
func generatedClientID() string {
	now := time.Now().UnixNano()

	for {
		last := lastClientStamp.Load()

		next := max(now, last+1)
		if lastClientStamp.CompareAndSwap(last, next) {
			return "prusa-server-" + strconv.FormatInt(next, 10)
		}
	}
}

// configFrom reads the operator's settings. Only the broker URL is required.
func configFrom(raw map[string]any) (Config, error) {
	cfg := Config{
		QueueDepth:     defaultQueueDepth,
		Keepalive:      defaultKeepalive,
		ConnectTimeout: defaultConnectTimeout,
		PublishTimeout: defaultPublishTimeout,
		DefaultAction:  ActionForward,
	}

	cfg.URL, _ = raw["url"].(string)
	if cfg.URL == "" {
		return Config{}, ErrMissingURL
	}

	cfg.ClientID, _ = raw["client_id"].(string)
	if cfg.ClientID == "" {
		cfg.ClientID = generatedClientID()
	}

	cfg.Username, _ = raw["username"].(string)
	cfg.Password, _ = raw["password"].(string)
	cfg.TLSSkipVerify, _ = raw["tls_skip_verify"].(bool)

	if depth, ok := intFrom(raw["queue_depth"]); ok {
		cfg.QueueDepth = depth
	}

	if cfg.QueueDepth < 1 {
		return Config{}, fmt.Errorf("mqtt: queue_depth must be at least 1, got %d", cfg.QueueDepth)
	}

	if err := durations(raw, &cfg); err != nil {
		return Config{}, err
	}

	if err := routing(raw, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func durations(raw map[string]any, cfg *Config) error {
	for key, target := range map[string]*time.Duration{
		"keepalive":       &cfg.Keepalive,
		"connect_timeout": &cfg.ConnectTimeout,
		"publish_timeout": &cfg.PublishTimeout,
	} {
		value, present := raw[key]
		if !present {
			continue
		}

		parsed, err := durationFrom(value)
		if err != nil {
			return fmt.Errorf("mqtt: config.%s: %w", key, err)
		}

		*target = parsed
	}

	return nil
}

func routing(raw map[string]any, cfg *Config) error {
	if action, ok := raw["default_action"].(string); ok && action != "" {
		if Action(action) != ActionForward && Action(action) != ActionDrop {
			return fmt.Errorf("mqtt: config.default_action must be %q or %q, got %q",
				ActionForward, ActionDrop, action)
		}

		cfg.DefaultAction = Action(action)
	}

	routes, ok := raw["routes"].([]any)
	if !ok {
		return nil
	}

	cfg.Routes = make([]Route, 0, len(routes))

	for i, entry := range routes {
		fields, isMap := entry.(map[string]any)
		if !isMap {
			return fmt.Errorf("mqtt: config.routes[%d] must be a mapping with a 'from' key", i)
		}

		from, _ := fields["from"].(string)
		to, _ := fields["to"].(string)
		action, _ := fields["action"].(string)

		cfg.Routes = append(cfg.Routes, Route{From: from, To: to, Action: Action(action)})
	}

	return nil
}

// intFrom accepts an int or a float64. A number from YAML is either,
// depending on the decoder.
func intFrom(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// durationFrom accepts "30s" and a bare number of seconds.
func durationFrom(value any) (time.Duration, error) {
	if text, ok := value.(string); ok {
		parsed, err := time.ParseDuration(text)
		if err != nil {
			return 0, fmt.Errorf("parse duration: %w", err)
		}

		return parsed, nil
	}

	seconds, ok := intFrom(value)
	if !ok {
		return 0, fmt.Errorf("expected a duration such as \"30s\" or a number of seconds, got %T", value)
	}

	return time.Duration(seconds) * time.Second, nil
}
