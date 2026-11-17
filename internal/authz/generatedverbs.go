package authz

// The generated half of the verb table: the printer's own verbs, keyed to the
// device kind `printer`. It is a static Go table, reviewed in pull requests,
// and it is one kind among others and not the whole vocabulary.

// generatedVerb is one row of the generated half: the verb, the class it
// carries and the markings the host puts on it. The class is declared and never
// derived: a verb's name says what it does to a machine and says nothing about
// the risk of doing it.
type generatedVerb struct {
	verb     string
	class    Action
	markings []Marking
}

// generatedVerbs is every verb the printer's published capability vocabulary
// names, with the class it carries. It is total over kind `printer`:
// ActionForVerb returns the zero Action for a verb this table does not name,
// and there is no default arm.
//
// A default arm would decide for every verb written after it, and nobody has
// weighed those verbs. TestEveryPublishedVerbTakesADecidedAction holds this
// table against internal/printer/profile in both directions, so a new verb
// fails the build until somebody names its class here.
//
// `operate` is the answer for most verbs, and it is a decision per verb and
// not a fallback. A class names what a verb is for. A reset destroys a print
// in progress and is still not a configuration change.
//
//nolint:gochecknoglobals // the vocabulary's own table; read-only after init.
var generatedVerbs = []generatedVerb{
	// The job. Stopping one is `operate` with the rest. It destroys work in
	// progress and wastes the material already laid down, which pausing does
	// not. It carries no marking, because nothing on the machine breaks.
	{verb: "core.command.start_print", class: ActionOperate},
	{verb: "core.command.pause_print", class: ActionOperate},
	{verb: "core.command.resume_print", class: ActionOperate},
	{verb: "core.command.stop_print", class: ActionOperate},

	// A file arriving on the printer's own storage. The printer fetches it, so
	// the verb reads as a download and what it does is an upload.
	{verb: "core.command.start_connect_download", class: ActionOperate},
	{verb: "core.command.start_encrypted_download", class: ActionOperate},
	{verb: "core.command.start_inline_download", class: ActionOperate},

	// Raw gcode. The class is `operate` because there is no lower class, and
	// the marking restricts it: a payload the host cannot read is conferred by
	// no class, role or level, and reaching it takes a row naming the verb. An
	// owner reaches it on their own machine.
	{verb: verbSendGCode, class: ActionOperate, markings: []Marking{MarkingOpaquePayload}},
	{verb: verbForceGCode, class: ActionOperate, markings: []Marking{MarkingOpaquePayload}},

	// The machine itself: what it is, and not the work on it. Firmware cannot
	// be put back, a token hands the machine to another Connect account, and a
	// hostname is the address every other caller reaches it at. So they are
	// marked irreversible as well as classed `configure`. The class puts them
	// above operator, and the tier puts them above the manager role too.
	{verb: "core.command.flash", class: ActionConfigure, markings: []Marking{MarkingIrreversible}},
	{verb: "core.command.upgrade", class: ActionConfigure, markings: []Marking{MarkingIrreversible}},
	{verb: "core.command.set_token", class: ActionConfigure, markings: []Marking{MarkingIrreversible}},
	{verb: "core.command.set_hostname", class: ActionConfigure, markings: []Marking{MarkingIrreversible}},

	// Everything else. A read of a machine is here too. Answering one puts a
	// command on a wire that carries one command at a time, so a caller who may
	// only read telemetry must not be able to queue work at somebody's printer.
	// For that reason `observe` is not a verb class: every verb is a write to
	// the transport, whatever it does to the device, so the floor is `operate`
	// and a destructive verb cannot be classed as a read.
	{verb: "core.command.beep", class: ActionOperate},
	{verb: "core.command.cancel_object", class: ActionOperate},
	{verb: "core.command.cancel_printer_ready", class: ActionOperate},
	{verb: "core.command.create_folder", class: ActionOperate},
	{verb: "core.command.delete_file", class: ActionOperate},
	{verb: "core.command.delete_folder", class: ActionOperate},
	{verb: "core.command.dialog_action", class: ActionOperate},
	{verb: "core.command.disable_steppers", class: ActionOperate},
	{verb: "core.command.home", class: ActionOperate},
	{verb: "core.command.load_filament", class: ActionOperate},
	{verb: "core.command.mesh_bed_leveling", class: ActionOperate},
	{verb: "core.command.move", class: ActionOperate},
	{verb: "core.command.move_e", class: ActionOperate},
	{verb: "core.command.move_z", class: ActionOperate},
	{verb: "core.command.reset", class: ActionOperate},
	{verb: "core.command.reset_printer", class: ActionOperate},
	{verb: "core.command.send_file_info", class: ActionOperate},
	{verb: "core.command.send_info", class: ActionOperate},
	{verb: "core.command.send_job_info", class: ActionOperate},
	{verb: "core.command.send_state_info", class: ActionOperate},
	{verb: "core.command.send_transfer_info", class: ActionOperate},
	{verb: "core.command.set_addon_power", class: ActionOperate},
	{verb: "core.command.set_chamber_fan_target", class: ActionOperate},
	{verb: "core.command.set_chamber_led_intensity", class: ActionOperate},
	{verb: "core.command.set_chamber_target_temp", class: ActionOperate},
	{verb: "core.command.set_enclosure_enabled", class: ActionOperate},
	{verb: "core.command.set_enclosure_postprint", class: ActionOperate},
	{verb: "core.command.set_enclosure_postprint_filtration_time", class: ActionOperate},
	{verb: "core.command.set_enclosure_printing_filtration", class: ActionOperate},
	{verb: "core.command.set_flow", class: ActionOperate},
	{verb: "core.command.set_bed_temperature", class: ActionOperate},
	{verb: "core.command.set_idle", class: ActionOperate},
	{verb: "core.command.set_nozzle_temperature", class: ActionOperate},
	{verb: "core.command.set_printer_ready", class: ActionOperate},
	{verb: "core.command.set_speed", class: ActionOperate},
	{verb: "core.command.set_tool_hardened", class: ActionOperate},
	{verb: "core.command.set_tool_high_flow", class: ActionOperate},
	{verb: "core.command.set_tool_nozzle_diameter", class: ActionOperate},
	{verb: "core.command.stop_transfer", class: ActionOperate},
	{verb: "core.command.uncancel_object", class: ActionOperate},
	{verb: "core.command.unload_filament", class: ActionOperate},
	{verb: "core.command.z_power_move", class: ActionOperate},
}
