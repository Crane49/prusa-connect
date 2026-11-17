package connectorapi

// RetainedTextFloor is how many bytes of one free-text value a host promises to
// keep: MachineReason, one StatePatch path, and the text inside one StatePatch
// value. It is a floor, not a limit. A driver may send more, and the host
// truncates and marks what it does not keep.
const RetainedTextFloor = 32768
