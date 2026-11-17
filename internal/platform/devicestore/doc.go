// Package devicestore keeps what the host remembers about its devices in the
// host's own database: each device's record, its prints, and the registrations
// the connectors admit it through. This package owns the tables' schema
// (Migrations), and the host applies it under its one ledger.
//
// The device directory is internal/authz's, and it is what says which devices
// exist. device_record and printer_job carry a foreign key onto authz_device, so
// a record or a print cannot name a device the directory does not hold, and
// removing a device takes them with it through ON DELETE CASCADE. Nothing here
// lists devices; the listing is the directory's.
package devicestore
