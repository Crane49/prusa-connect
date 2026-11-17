package app

import (
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// auditAction is the action a principal must hold on a device to receive that
// device's audit-class events, or to have a question about them answered. It is
// view_audit, which the manager role confers and no lower role does.
func auditAction() authz.Action { return authz.ActionViewAudit }
