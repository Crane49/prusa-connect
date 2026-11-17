package httpfault

import (
	"net/http"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// A printer the server runs on is refused a retirement, because its driver
// connects again at once and the machine comes back under a new id.
var deviceResident = fault.Register(
	fault.DomainPrinter(), "DEVICE_RESIDENT", http.StatusConflict, fault.Document(
		"The printer runs this server, so it cannot be removed from it",
		"Keep the printer. Its record says resident: true. To stop using this server, switch it off on the printer.",
		fault.RetryNever,
		fault.ShowDetail,
	))

// DeviceResident reports a retirement of a printer the server runs on.
func DeviceResident() *fault.Fault {
	return deviceResident.New("the printer runs this server and cannot be retired")
}

// CodeDeviceResident names the code for a retirement of a resident printer.
func CodeDeviceResident() fault.Code { return deviceResident.Code() }

// A read of a printer folder that the printer did not answer: the connection
// went or the read outlived its bound. Nothing about the folder is known, and
// asking again is safe because a listing changes nothing. Only the host bounds
// a read, so no driver returns it.
var listingUnfinished = fault.Register(
	fault.DomainPrinter(), "LISTING_UNFINISHED", http.StatusConflict, fault.Document(
		"The printer did not finish reading the folder",
		"Ask for the folder again once the printer is connected. An MK4S takes 12 to 15 s to list a "+
			"USB root of about 300 files, and a read still running after two minutes is stopped.",
		fault.RetryAllowed,
		fault.ShowDetail,
		"path",
	))

// ListingUnfinished reports a read of folder that the printer did not finish.
func ListingUnfinished(folder string) *fault.Fault {
	return listingUnfinished.New("the printer did not finish reading the folder").
		With(fault.StringParam("path", folder))
}

// CodeListingUnfinished names the code for a read the printer did not finish.
func CodeListingUnfinished() fault.Code { return listingUnfinished.Code() }
