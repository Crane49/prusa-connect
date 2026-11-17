package connectorapi

// The state paths where a driver reports the space a print may fill, in
// millimetres. The host has no table of machines. A driver that does not know
// the model reports nothing, and a reader treats that as unknown.
const (
	// BuildVolumeXPath and BuildVolumeYPath are the width and the depth of
	// the area a print may cover on the bed, measured from the bed's origin.
	BuildVolumeXPath = "/build_volume/x"
	BuildVolumeYPath = "/build_volume/y"
	// BuildVolumeZPath is the highest a print may reach above the bed.
	BuildVolumeZPath = "/build_volume/z"
)
