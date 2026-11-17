package fault

// The core.* codes are registered in this package, one file per domain. These
// are the advisory HTTP statuses they use.
const (
	statusBadRequest        = 400
	statusUnauthorized      = 401
	statusForbidden         = 403
	statusNotFound          = 404
	statusConflict          = 409
	statusTooManyRequests   = 429
	statusUnsupportedType   = 415
	statusUnprocessable     = 422
	statusInsufficientSpace = 507
	statusUnavailable       = 503
)
