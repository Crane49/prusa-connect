package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The errors ParseConfig returns when it refuses the settings.
var (
	// ErrNoNodeID means the settings have no node_id.
	ErrNoNodeID = errors.New("node_id is required")
	// ErrNoDataDir means the settings have no data_dir, the directory where
	// the node stores its messages and its keys.
	ErrNoDataDir = errors.New("data_dir is required")
	// ErrBadDuration means a duration is not in the format of
	// time.ParseDuration, such as "5m" or "500ms".
	ErrBadDuration = errors.New("the value is not a length of time")
	// ErrNoAdvertise means the accept block has no advertise address for the
	// servers below to connect to.
	ErrNoAdvertise = errors.New("accept.advertise is required on a server that accepts others under it")
	// ErrBadAdvertise means accept.advertise has no scheme or no host. The
	// servers below dial that address, so it must parse as a URL.
	ErrBadAdvertise = errors.New("accept.advertise must be written as a scheme and a host, such as tls://host:7422")
	// ErrNoJWKSURL means accept.verification is "jwt" and accept.jwks_url,
	// the address of the public keys that check a token, is empty.
	ErrNoJWKSURL = errors.New(`accept.jwks_url is required when accept.verification is "jwt"`)
	// ErrBadCommandPrincipal means command_principal is neither "node" nor
	// "self". The host would deny every command under any other value.
	ErrBadCommandPrincipal = errors.New(`command_principal is "node" or "self"`)
	// ErrNoBaseURL means the files block has no base_url, the address the
	// module puts in every grant.
	ErrNoBaseURL = errors.New("files.base_url is required when the files block is set")
	// ErrBadBaseURL means files.base_url is not an absolute https URL with no
	// query and no fragment. http passes only with allow_cleartext_parent.
	ErrBadBaseURL = errors.New("files.base_url must be an absolute https URL with no query and no fragment")
	// ErrNoFilesPort means the settings have a files block and the operator
	// granted no TCP port named files.
	ErrNoFilesPort = errors.New("the files block needs the port files, and it is not granted")
	// ErrNoDataGrant means the settings have a files block and the operator
	// granted no data directory.
	ErrNoDataGrant = errors.New("the files block needs the data directory of the module, and it is not granted")
	// ErrBaseURLPort means files.base_url names another port than the one
	// granted as files.
	ErrBaseURLPort = errors.New("files.base_url is not on the port granted as files")
	// ErrFilesNeedTLS means files.base_url is https and the tls block names no
	// certificate and key to serve it with.
	ErrFilesNeedTLS = errors.New("files.base_url is https, and the tls block names no certificate and key")
	// ErrNoFilesEntry means the grant of the server above has no files
	// entry, so the server above does not run the file extension.
	ErrNoFilesEntry = errors.New("the server above does not run the files extension")
)

// Config is the settings of this module, as an operator writes them. The host
// sends them with plugin.configure at every start. A duration is a string for
// time.ParseDuration. An empty duration or a zero number means the default of
// the transport or of the bridge.
type Config struct {
	// NodeID is the name this server asks for in the network. A server with no
	// server above it uses NodeID as its whole Node Path.
	NodeID string `json:"node_id"`
	// DataDir is the directory where this node stores its waiting messages,
	// its record of the method calls it already ran, and its keys.
	DataDir string `json:"data_dir"`
	// Identity is the secret seed of this node's key, in the NKEY text format
	// of NATS. Empty means a seed kept in DataDir, made at the first start.
	Identity string `json:"identity"`

	// Join names the server this one joins. It is nil on a server that has no
	// server above it.
	Join *JoinConfig `json:"join"`
	// Accept says how this server accepts other servers and printers under it.
	// It is nil on a server that accepts none.
	Accept *AcceptConfig `json:"accept"`
	// TLS names the certificate files. Nil turns TLS off. Turn it off only
	// where something else protects the connection.
	TLS *TLSConfig `json:"tls"`
	// Shadow selects the interfaces of the nodes below that this server keeps
	// a local copy of. That copy is the shadow.
	Shadow ShadowConfig `json:"shadow"`

	// DedupWindow is how long this node remembers a method call, so a call
	// that arrives twice runs once. Required. Set it longer than the longest
	// expected outage plus the longest deadline of a call.
	DedupWindow string `json:"dedup_window"`
	// CredentialLifetime is how long the connection credentials this node
	// gives to the servers below it stay valid.
	CredentialLifetime string `json:"credential_lifetime"`
	// HeartbeatInterval is how often this node tells the server above that it
	// is alive.
	HeartbeatInterval string `json:"heartbeat_interval"`
	// MaxHeartbeatInterval is the longest time between two heartbeats that
	// this node allows the servers below it.
	MaxHeartbeatInterval string `json:"max_heartbeat_interval"`
	// LivenessMultiple is how many MaxHeartbeatInterval periods may pass with
	// no heartbeat before this node counts a server below it as gone.
	LivenessMultiple int `json:"liveness_multiple"`
	// AckWait is how long the message store waits for a receiver to confirm a
	// message before it delivers the message again.
	AckWait string `json:"ack_wait"`
	// StreamMaxAge is how long a message waits for a node that is not
	// connected before the message store drops it.
	StreamMaxAge string `json:"stream_max_age"`
	// StreamMaxBytes is the most bytes one message store keeps.
	StreamMaxBytes int64 `json:"stream_max_bytes"`
	// MaxPayload is the largest size of one message on the network, in bytes.
	MaxPayload int32 `json:"max_payload"`
	// PollInterval is how often this node checks which servers below it are
	// connected.
	PollInterval string `json:"poll_interval"`

	// FlushInterval is how often the bridge sends the printer readings that
	// changed to the server above.
	FlushInterval string `json:"flush_interval"`
	// ResyncInterval is how often the bridge reads the whole list of printers
	// again.
	ResyncInterval string `json:"resync_interval"`
	// Grants is the ports and the data directory the operator granted. The
	// host sends it beside the settings. No key of the settings sets it.
	Grants wirerpc.Grants `json:"-"`

	// Files turns the file extension on. It is nil when the settings have no
	// files block, and the module then neither publishes nor fetches a file.
	Files *FilesConfig `json:"files"`

	// CommandPrincipal selects whose permissions the host checks for a command
	// from above: PrincipalNode, the default, or PrincipalSelf.
	CommandPrincipal string `json:"command_principal"`
}

// JoinConfig names the server this one joins.
type JoinConfig struct {
	// Address is where this node sends its join request: the ClientListen
	// address of the server above.
	Address string `json:"address"`
	// Credential is the token this node sends with its join request. Only a
	// server above that checks tokens needs it.
	Credential string `json:"credential"`
}

// AcceptConfig says how this server accepts other servers and printers under
// it.
type AcceptConfig struct {
	// Advertise is the address of LeafListen as the servers below reach it,
	// for example "tls://workshop.example:7422".
	Advertise string `json:"advertise"`
	// ClientListen is the address where this server receives join requests.
	ClientListen string `json:"client_listen"`
	// LeafListen is the address where the servers below keep their connections
	// open.
	LeafListen string `json:"leaf_listen"`
	// Verification is "tofu", which trusts the key a server presents the
	// first time, or "jwt", which checks a signed token. Empty means "tofu".
	Verification string `json:"verification"`
	// AskFirst makes every server this one has not seen before wait until a
	// person approves it on the links page.
	AskFirst bool `json:"ask_first"`
	// JWKSURL is the address of the public keys that check a token under
	// "jwt". It is required under "jwt", and unused under "tofu".
	JWKSURL string `json:"jwks_url"`
	// MaxPendingNodes is the most servers that may wait for approval at the
	// same time.
	MaxPendingNodes int `json:"max_pending_nodes"`
	// MaxJoinsInFlight is the most join requests this server checks at the
	// same time.
	MaxJoinsInFlight int `json:"max_joins_in_flight"`
	// JoinsPerSecond is the most join requests this server accepts per second.
	JoinsPerSecond float64 `json:"joins_per_second"`
	// PendingExpires is how long a server that waits for approval stays in the
	// list. Anyone who reaches the join port can fill that list.
	PendingExpires string `json:"pending_expires"`
}

// TLSConfig names the files of the certificate this node presents, its private
// key, and the certificate authorities it checks the server above against.
type TLSConfig struct {
	CertFile string   `json:"cert_file"`
	KeyFile  string   `json:"key_file"`
	CAFiles  []string `json:"ca_files"`
}

// The defaults of the files block, applied to a key the operator left out.
const (
	DefaultRescan         = 60 * time.Second
	DefaultRelayTimeout   = 30 * time.Second
	DefaultProofTolerance = 60 * time.Second
	DefaultMaxResumes     = 5
	DefaultFetchAttempts  = 3
	DefaultStartDeadline  = 10 * time.Minute
	DefaultCacheMaxBytes  = 10 << 30
)

// FilesConfig is the files block. Its durations are written as text, such as
// "60s", like every other duration of this module.
type FilesConfig struct {
	// BaseURL is the address this server puts in every grant, where the
	// servers below fetch a file from it.
	BaseURL string `json:"base_url"`
	// AllowCleartextParent lets this server take a server above whose base
	// URL is http. Both ends then decide that TLS is off.
	AllowCleartextParent bool `json:"allow_cleartext_parent"`
	// Rescan is how often the module scans the shared folders again.
	Rescan time.Duration `json:"rescan"`
	// RelayTimeout is how long a request to the server above may take before
	// the module answers 502.
	RelayTimeout time.Duration `json:"relay_timeout"`
	// ProofTolerance is how far the time in a proof may be from this server's
	// clock.
	ProofTolerance time.Duration `json:"proof_tolerance"`
	// MaxResumes is how often the module resumes one cut transfer.
	MaxResumes int `json:"max_resumes"`
	// FetchAttempts is how often the module fetches a file for one print.
	FetchAttempts int `json:"fetch_attempts"`
	// StartDeadline is how long a print may wait for its file before the
	// module drops it.
	StartDeadline time.Duration `json:"start_deadline"`
	// CacheMaxBytes is the most bytes the copies of files from above take.
	CacheMaxBytes int64 `json:"cache_max_bytes"`
}

// UnmarshalJSON reads the files block: durations as text, and a default for
// every key left out. It refuses an unknown key itself, because the decoder of
// the whole settings does not check inside a value that decodes itself.
func (f *FilesConfig) UnmarshalJSON(data []byte) error {
	var written struct {
		BaseURL              string `json:"base_url"`
		AllowCleartextParent bool   `json:"allow_cleartext_parent"`
		Rescan               string `json:"rescan"`
		RelayTimeout         string `json:"relay_timeout"`
		ProofTolerance       string `json:"proof_tolerance"`
		MaxResumes           int    `json:"max_resumes"`
		FetchAttempts        int    `json:"fetch_attempts"`
		StartDeadline        string `json:"start_deadline"`
		CacheMaxBytes        int64  `json:"cache_max_bytes"`
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&written); err != nil {
		return ConfigValueInvalid.New("this module could not read the files block").
			With(fault.StringParam(paramField, "files")).Wrap(err)
	}

	held := FilesConfig{
		BaseURL:              written.BaseURL,
		AllowCleartextParent: written.AllowCleartextParent,
		MaxResumes:           orDefault(written.MaxResumes, DefaultMaxResumes),
		FetchAttempts:        orDefault(written.FetchAttempts, DefaultFetchAttempts),
		CacheMaxBytes:        orDefault(written.CacheMaxBytes, DefaultCacheMaxBytes),
	}

	for _, field := range []struct {
		name     string
		text     string
		fallback time.Duration
		where    *time.Duration
	}{
		{name: "files.rescan", text: written.Rescan, fallback: DefaultRescan, where: &held.Rescan},
		{name: "files.relay_timeout", text: written.RelayTimeout, fallback: DefaultRelayTimeout, where: &held.RelayTimeout},
		{name: "files.proof_tolerance", text: written.ProofTolerance, fallback: DefaultProofTolerance, where: &held.ProofTolerance},
		{name: "files.start_deadline", text: written.StartDeadline, fallback: DefaultStartDeadline, where: &held.StartDeadline},
	} {
		parsed, err := duration(field.name, field.text)
		if err != nil {
			return err
		}

		*field.where = orDefault(parsed, field.fallback)
	}

	*f = held

	return nil
}

// orDefault returns fallback for a value that is zero or less.
func orDefault[T int | int64 | time.Duration](value, fallback T) T {
	if value <= 0 {
		return fallback
	}

	return value
}

// joinEntry is the files entry this server puts in every grant.
func (f FilesConfig) joinEntry() (json.RawMessage, error) {
	entry, err := fractalfiles.JoinEntry{
		Version: 1,
		HTTP:    &fractalfiles.HTTPEntry{Version: 1, BaseURL: f.BaseURL},
	}.Marshal()
	if err != nil {
		return nil, fmt.Errorf("encode the files entry of the grant: %w", err)
	}

	return entry, nil
}

// checkGrant refuses a grant whose files entry this server cannot use: none,
// an unsupported version, no http part, or a base URL it may not dial.
func (f FilesConfig) checkGrant(held map[fractal.Identifier]json.RawMessage) error {
	raw, ok := held[fractalfiles.ExtensionName]
	if !ok {
		return ErrNoFilesEntry
	}

	if _, err := fractalfiles.ParseJoinEntry(raw, f.AllowCleartextParent); err != nil {
		return fmt.Errorf("the files entry of the server above: %w", err)
	}

	return nil
}

// contentStores selects ContentStore1 on every node above and below. A server
// that runs the file extension must shadow both.
const contentStores fractal.Pattern = fractalfiles.InterfaceName

// ancestorShadow is the configuration of the copy of the state above. It
// selects nothing when the file extension is off.
func (c Config) ancestorShadow() shadow.Config {
	if c.Files == nil {
		return shadow.Config{}
	}

	return shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{contentStores}}}
}

// ShadowConfig selects the interfaces of the nodes below that this server
// keeps a local copy of.
type ShadowConfig struct {
	// Patterns select interfaces by name on every node below. A "*" matches
	// any characters, so the pattern "*" selects every interface.
	Patterns []string `json:"patterns"`
	// Interfaces select single interfaces, each on one node.
	Interfaces []ShadowInterfaceConfig `json:"interfaces"`
}

// ShadowInterfaceConfig names one interface instance of one node below.
type ShadowInterfaceConfig struct {
	Node      string `json:"node"`
	Interface string `json:"interface"`
	Instance  string `json:"instance"`
}

// ParseConfig decodes and checks the settings the host sent. It refuses a
// document with an unknown key. Every refusal has a registered code and names
// the key it is about.
func ParseConfig(data []byte) (Config, error) {
	var held Config

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&held); err != nil {
		// A block that reads itself, such as files, already names its key.
		if _, coded := errors.AsType[*fault.Fault](err); coded {
			return Config{}, err
		}

		return Config{}, ConfigValueInvalid.New("this module could not read the settings").
			With(fault.StringParam(paramField, "config")).Wrap(err)
	}

	if held.NodeID == "" {
		return Config{}, missingField("node_id", ErrNoNodeID)
	}

	if held.DataDir == "" {
		return Config{}, missingField("data_dir", ErrNoDataDir)
	}

	if held.Accept != nil && held.Accept.Advertise == "" {
		return Config{}, missingField("accept.advertise", ErrNoAdvertise)
	}

	if held.Accept != nil {
		advertised, err := url.Parse(held.Accept.Advertise)
		if err != nil || advertised.Scheme == "" || advertised.Hostname() == "" {
			return Config{}, ConfigValueInvalid.New(ErrBadAdvertise.Error()).
				With(fault.StringParam(paramField, "accept.advertise")).Wrap(ErrBadAdvertise)
		}
	}

	switch held.CommandPrincipal {
	case "", PrincipalNode, PrincipalSelf:
	default:
		return Config{}, ConfigValueInvalid.New(ErrBadCommandPrincipal.Error()).
			With(fault.StringParam(paramField, "command_principal")).Wrap(ErrBadCommandPrincipal)
	}

	if held.Accept != nil && held.Accept.Verification == string(natsbind.ModeJWT) && held.Accept.JWKSURL == "" {
		return Config{}, missingField("accept.jwks_url", ErrNoJWKSURL)
	}

	if err := held.checkFiles(); err != nil {
		return Config{}, err
	}

	if _, err := held.Binding(); err != nil {
		return Config{}, err
	}

	// The bridge reads these two. Checking them here reports a mistake at
	// the start.
	if _, err := held.Flush(); err != nil {
		return Config{}, err
	}

	if _, err := held.Resync(); err != nil {
		return Config{}, err
	}

	return held, nil
}

// checkFiles refuses a files block whose base_url the servers below could not
// use. A base_url of http passes only with allow_cleartext_parent.
func (c Config) checkFiles() error {
	if c.Files == nil {
		return nil
	}

	if c.Files.BaseURL == "" {
		return missingField("files.base_url", ErrNoBaseURL)
	}

	if err := fractalfiles.CheckBaseURL(c.Files.BaseURL, c.Files.AllowCleartextParent); err != nil {
		return ConfigValueInvalid.New(ErrBadBaseURL.Error()).
			With(fault.StringParam(paramField, "files.base_url")).Wrap(errors.Join(ErrBadBaseURL, err))
	}

	return nil
}

// filesPort is the name of the port the module serves the files on, as
// module.yaml asks for it in needs.listen.
const filesPort = "files"

// checkGrants refuses a files block without the TCP port files and the data
// directory. base_url must be on that port, and https needs the tls block.
func (c Config) checkGrants() error {
	if c.Files == nil {
		return nil
	}

	port, granted := c.Grants.Listen[filesPort]
	if !granted || port.Network != "tcp" {
		return GrantMissing.New(ErrNoFilesPort.Error()).
			With(fault.StringParam(paramName, filesPort)).Wrap(ErrNoFilesPort)
	}

	if c.Grants.Data == "" {
		return GrantMissing.New(ErrNoDataGrant.Error()).
			With(fault.StringParam(paramName, "data")).Wrap(ErrNoDataGrant)
	}

	base, err := url.Parse(c.Files.BaseURL)
	if err != nil {
		return ConfigValueInvalid.New(ErrBadBaseURL.Error()).
			With(fault.StringParam(paramField, "files.base_url")).Wrap(errors.Join(ErrBadBaseURL, err))
	}

	if portOf(base) != strconv.Itoa(port.Port) {
		return ConfigValueInvalid.New(fmt.Sprintf("%s: it names %s and the port files is %d",
			ErrBaseURLPort, portOf(base), port.Port)).
			With(fault.StringParam(paramField, "files.base_url")).Wrap(ErrBaseURLPort)
	}

	if base.Scheme == "https" && (c.TLS == nil || c.TLS.CertFile == "" || c.TLS.KeyFile == "") {
		return ConfigValueInvalid.New(ErrFilesNeedTLS.Error()).
			With(fault.StringParam(paramField, "tls")).Wrap(ErrFilesNeedTLS)
	}

	return nil
}

// portOf returns the port of a URL, and the default port of its scheme when
// it names none.
func portOf(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}

	if u.Scheme == "http" {
		return "80"
	}

	return "443"
}

// missingField builds the refusal for a required setting. The sentinel error
// stays the cause, so errors.Is still matches it.
func missingField(key string, cause error) error {
	return ConfigFieldMissing.New(cause.Error()).
		With(fault.StringParam(paramField, key)).Wrap(cause)
}

// Binding converts the settings into the configuration of the transport. It
// leaves an empty setting at zero, and the transport applies its own default.
// The one default it sets is the verification mode "tofu".
func (c Config) Binding() (natsbind.Config, error) {
	cfg := natsbind.Config{
		NodeID:          fractal.NodeID(c.NodeID),
		DataDir:         c.DataDir,
		AcceptsChildren: c.Accept != nil,
		StreamMaxBytes:  c.StreamMaxBytes,
		MaxPayload:      c.MaxPayload,
	}

	if err := c.durations(&cfg); err != nil {
		return natsbind.Config{}, err
	}

	if c.Join != nil {
		cfg.ParentAddr = c.Join.Address
		cfg.JoinCredential = fractal.JoinCredential(c.Join.Credential)
	}

	if c.Accept != nil {
		cfg.AdvertisedLeafRemote = c.Accept.Advertise
		cfg.ClientListen = c.Accept.ClientListen
		cfg.LeafListen = c.Accept.LeafListen
		cfg.Verification = natsbind.Mode(c.Accept.Verification)
		cfg.AskFirst = c.Accept.AskFirst
		cfg.MaxPendingNodes = c.Accept.MaxPendingNodes
		cfg.MaxJoinsInFlight = c.Accept.MaxJoinsInFlight
		cfg.JoinsPerSecond = c.Accept.JoinsPerSecond

		expires, err := duration("accept.pending_expires", c.Accept.PendingExpires)
		if err != nil {
			return natsbind.Config{}, err
		}

		cfg.PendingExpires = expires

		if cfg.Verification == "" {
			cfg.Verification = natsbind.ModeTOFU
		}

		if cfg.Verification == natsbind.ModeJWT {
			cfg.KeySource = joinwire.NewJWKS(c.Accept.JWKSURL)
		}
	}

	if c.TLS != nil {
		cfg.TLS = &natsbind.TLSMaterial{
			CertFile: c.TLS.CertFile,
			KeyFile:  c.TLS.KeyFile,
			CAFiles:  c.TLS.CAFiles,
		}
	}

	cfg.LivenessMultiple = c.LivenessMultiple

	if c.Files != nil {
		entry, err := c.Files.joinEntry()
		if err != nil {
			return natsbind.Config{}, err
		}

		cfg.Extensions = map[fractal.Identifier]json.RawMessage{fractalfiles.ExtensionName: entry}
		cfg.JoinExtensions = []fractal.Identifier{fractalfiles.ExtensionName}
		cfg.CheckExtensions = c.Files.checkGrant
	}

	selection, err := c.Shadow.selection()
	if err != nil {
		return natsbind.Config{}, err
	}

	// A node that browses the stores below it selects ContentStore1 on every
	// node below.
	if c.Files != nil {
		selection.Patterns = append(selection.Patterns, contentStores)
	}

	cfg.Shadow = shadow.Config{Selection: selection}

	return cfg, nil
}

// durations parses every duration setting of the transport into cfg.
func (c Config) durations(cfg *natsbind.Config) error {
	for _, field := range []struct {
		name  string
		text  string
		where *time.Duration
	}{
		{name: "dedup_window", text: c.DedupWindow, where: &cfg.DedupWindow},
		{name: "credential_lifetime", text: c.CredentialLifetime, where: &cfg.CredentialLifetime},
		{name: "heartbeat_interval", text: c.HeartbeatInterval, where: &cfg.HeartbeatInterval},
		{name: "max_heartbeat_interval", text: c.MaxHeartbeatInterval, where: &cfg.MaxHeartbeatInterval},
		{name: "ack_wait", text: c.AckWait, where: &cfg.AckWait},
		{name: "stream_max_age", text: c.StreamMaxAge, where: &cfg.StreamMaxAge},
		{name: "poll_interval", text: c.PollInterval, where: &cfg.PollInterval},
	} {
		held, err := duration(field.name, field.text)
		if err != nil {
			return err
		}

		*field.where = held
	}

	return nil
}

// Flush returns FlushInterval as a duration, and zero when it is empty.
func (c Config) Flush() (time.Duration, error) {
	return duration("flush_interval", c.FlushInterval)
}

// Resync returns ResyncInterval as a duration, and zero when it is empty.
func (c Config) Resync() (time.Duration, error) {
	return duration("resync_interval", c.ResyncInterval)
}

// duration parses one duration setting. An empty text gives zero, which the
// transport and the bridge read as the default.
func duration(name, text string) (time.Duration, error) {
	if text == "" {
		return 0, nil
	}

	held, err := time.ParseDuration(text)
	if err != nil {
		return 0, ConfigDurationInvalid.New(fmt.Sprintf("%s is %q", name, text)).
			With(fault.StringParam(paramField, name)).Wrap(ErrBadDuration)
	}

	return held, nil
}

// selection parses the patterns and the interface names into a
// shadow.Selection, and refuses one that is not valid.
func (s ShadowConfig) selection() (shadow.Selection, error) {
	selection := shadow.Selection{}

	for _, text := range s.Patterns {
		pattern, err := fractal.ParsePattern(text)
		if err != nil {
			return shadow.Selection{}, ConfigValueInvalid.
				New(fmt.Sprintf("shadow.patterns holds %q, which is not a pattern", text)).
				With(fault.StringParam(paramField, "shadow.patterns")).Wrap(err)
		}

		selection.Patterns = append(selection.Patterns, pattern)
	}

	for _, named := range s.Interfaces {
		selection.Interfaces = append(selection.Interfaces, shadow.Key{
			Node:      fractal.NodePath(named.Node),
			Interface: fractal.InterfaceName(named.Interface),
			Instance:  fractal.InstanceID(named.Instance),
		})
	}

	if err := selection.Validate(); err != nil {
		return shadow.Selection{}, ConfigValueInvalid.New("the shadow selection is not one this module can apply").
			With(fault.StringParam(paramField, "shadow")).Wrap(err)
	}

	return selection, nil
}
