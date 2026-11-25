package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/moduledeps"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

// moduleImpacts predicts what the upgrade does to every installed module: it
// keeps running, the module plan updates it, or it stops serving because no
// release of it fits the candidate core or the candidate's wire contract. An
// instance the server needs to start that would stop serving blocks the
// upgrade; the loader keeps that flag off every module, so a module alone
// never blocks it.
func (m *maintenance) moduleImpacts(metadata *upgrade.BinaryMetadata, planned modulePlan) []upgrade.ModuleImpact {
	document, err := resource.Load(resource.Paths{File: m.configPath, Overlay: m.core.Overlay})
	if err != nil {
		return []upgrade.ModuleImpact{{
			Module: "configuration", Outcome: upgrade.ImpactBlocked, NeededToStart: true, Reason: err.Error(),
		}}
	}

	contractMajor := 0
	if metadata != nil {
		contractMajor, _ = strconv.Atoi(strings.Split(metadata.Contract, ".")[0])
	}

	updated := make(map[string]moduledeps.Step, len(planned.steps))
	for _, step := range planned.steps {
		updated[step.Release.Namespace] = step
	}

	var impacts []upgrade.ModuleImpact
	for _, declared := range document.Instances {
		if declared.Spec.Service == nil || declared.Spec.Module == nil {
			continue
		}

		namespace, _ := declared.ModuleNamespace()
		impact := upgrade.ModuleImpact{
			Module: declared.Ref.String(), Outcome: upgrade.ImpactOK, NeededToStart: declared.Spec.Required,
		}

		switch step, found := updated[namespace]; {
		case found && step.Release.Version != declared.Spec.Module.Version:
			impact.Outcome = upgrade.ImpactUpdated
			impact.Reason = "the upgrade installs " + step.Release.Version + " beside the candidate core"
		case planned.refused != nil && slices.Contains(planned.broken, namespace):
			impact.Outcome = upgrade.ImpactDisabled
			impact.Reason = planned.refused.Error()
		default:
			if declared.Spec.Service.ContractMajor != contractMajor {
				impact.Outcome = upgrade.ImpactDisabled
				impact.Reason = "wire contract major does not match"
			}

			applyCapabilities(&impact, declared.Spec.Module.Requires, metadata)
		}

		if impact.NeededToStart && impact.Outcome == upgrade.ImpactDisabled {
			impact.Outcome = upgrade.ImpactBlocked
		}

		impacts = append(impacts, impact)
	}

	return impacts
}

// applyCapabilities disables a module whose require names a wire method the
// candidate does not serve. Its core constraint is the module plan's.
func applyCapabilities(
	impact *upgrade.ModuleImpact,
	requirements []install.Requirement,
	metadata *upgrade.BinaryMetadata,
) {
	for _, requirement := range requirements {
		if requirement.Capability != "" &&
			(metadata == nil || !slices.Contains(metadata.Capabilities, requirement.Capability)) {
			impact.Outcome = upgrade.ImpactDisabled
			impact.Reason = "candidate does not serve " + requirement.Capability
		}
	}
}

// installerMarker is the file an installer puts beside the executable it
// installed. The installer then owns every replacement of the executable, and
// the server prepares an upgrade but never swaps the file itself
// (ADR-20261125-jqp).
const installerMarker = "managed-by-installer"

// installerReason is what the Updates page shows when installerMarker is
// found.
const installerReason = "the installer owns the executable; run the installer of the new release"

func externallyManaged(binary string) (bool, string) {
	switch {
	case buildprofile.Name == "printer":
		return true, "the printer operating system owns the executable"
	case pathExists(filepath.Join(filepath.Dir(binary), installerMarker)):
		return true, installerReason
	case pathExists("/.dockerenv"):
		return true, "the container image owns the executable"
	case os.Getenv("KUBERNETES_SERVICE_HOST") != "":
		return true, "the Kubernetes workload owns the executable"
	case !writableDirectory(filepath.Dir(binary)):
		return true, "the executable directory is not writable"
	default:
		return false, ""
	}
}

func writableDirectory(path string) bool {
	probe, err := os.CreateTemp(path, ".upgrade-probe-*")
	if err != nil {
		return false
	}

	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)

	return true
}

func pathExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func upgradeCommandArgs(configPath string) []string {
	return []string{UpgradeCommand, "check", "-config", configPath, "-json"}
}

func (m *maintenance) diskBudget(journal upgrade.Journal) (int64, int64, error) {
	var required int64
	if info, err := os.Stat(journal.StagedBinary); err == nil {
		required += info.Size() * candidateDiskCopies
	}
	for _, extension := range journal.Extensions {
		required += extension.Size * candidateDiskCopies
	}

	plan, err := backupPlan(
		m.configPath, m.core, journal.ToVersion, upgrade.PostgresMode(m.core.Upgrade.PostgresBackup), true,
	)
	if err != nil {
		return 0, 0, err
	}
	for _, source := range plan.Files {
		info, statErr := os.Stat(source.Path)
		if statErr == nil {
			required += info.Size() * protectedDiskCopies
		}
	}

	available, err := availableDisk(filepath.Dir(upgradeJournalPath(m.core)))
	if err != nil {
		return 0, 0, fmt.Errorf("inspect upgrade disk space: %w", err)
	}

	return required, available, nil
}
