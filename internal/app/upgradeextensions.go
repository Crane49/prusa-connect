package app

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
	sdkextension "github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

func (m *maintenance) trialExtensions(
	journal upgrade.Journal,
	impacts []upgrade.ModuleImpact,
) []string {
	required := make(map[string]bool, len(journal.Extensions)+len(impacts))
	for _, extension := range journal.Extensions {
		instance, err := ref.New(ref.Kind(extension.Kind), extension.Name)
		if err == nil {
			required[instance.String()] = true
		}
	}
	if m.modules != nil {
		for _, impact := range impacts {
			if impact.Outcome != "ok" {
				continue
			}
			instance, err := ref.Parse(impact.Module)
			if err != nil {
				continue
			}
			if status, found := m.modules.Status(instance); found && status.Serving {
				required[impact.Module] = true
			}
		}
	}

	out := slices.Collect(maps.Keys(required))
	slices.Sort(out)

	return out
}

// reviewBundledExtensions checks every bundled archive the way an install
// would and says what each asks for, what the installed release did not ask
// for, and whether it is older than the installed one. Preparation stores the
// reviews in the journal and apply grants no more than they show.
func reviewBundledExtensions(
	core config.Core, configPath string, journal upgrade.Journal,
) ([]upgrade.ExtensionReview, error) {
	if len(journal.Extensions) == 0 {
		return nil, nil
	}
	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return nil, err
	}

	reviews := make([]upgrade.ExtensionReview, 0, len(journal.Extensions))
	for _, extension := range journal.Extensions {
		review, reviewErr := reviewBundledExtension(doc, journal.ToVersion, extension)
		if reviewErr != nil {
			return nil, fmt.Errorf("validate bundled extension %s@%s: %w",
				extension.Module, extension.Version, reviewErr)
		}
		reviews = append(reviews, review)
	}

	return reviews, nil
}

func reviewBundledExtension(
	doc resource.Document, toVersion string, staged upgrade.StagedExtension,
) (upgrade.ExtensionReview, error) {
	dir, done, err := stageRelease(staged.StagedPath)
	if err != nil {
		return upgrade.ExtensionReview{}, err
	}
	defer done()

	held, err := readRelease(dir)
	if err != nil {
		return upgrade.ExtensionReview{}, err
	}
	if err = releaseMatches(staged.Module+versionSeparator+staged.Version, staged.Version, held.manifest); err != nil {
		return upgrade.ExtensionReview{}, err
	}
	if err = coreMatchesVersion(held.manifest, toVersion); err != nil {
		return upgrade.ExtensionReview{}, err
	}
	if err = requirementsMeetableVersion(held.manifest, toVersion); err != nil {
		return upgrade.ExtensionReview{}, err
	}
	instance, err := ref.New(ref.Kind(held.manifest.Kind), held.manifest.Name)
	if err != nil {
		return upgrade.ExtensionReview{}, err
	}
	if err = installableUnder(instance.Kind(), held.manifest); err != nil {
		return upgrade.ExtensionReview{}, err
	}
	if _, err = verifyChecksums(dir); err != nil {
		return upgrade.ExtensionReview{}, err
	}
	namespace, err := sdkextension.NamespaceOf(staged.Module)
	if err != nil {
		return upgrade.ExtensionReview{}, fmt.Errorf("%s names no namespace: %w", staged.Module, err)
	}

	asked := asksOf(held.manifest)
	shown, err := json.Marshal(asked)
	if err != nil {
		return upgrade.ExtensionReview{}, fmt.Errorf("record the asks: %w", err)
	}
	review := upgrade.ExtensionReview{
		Module: staged.Module, Version: held.manifest.Version,
		Asks:    beyondAsks(asked, install.ModuleAsks{}, namespace.String()),
		NewAsks: beyondAsks(asked, install.ModuleAsks{}, namespace.String()),
		Shown:   shown,
	}
	declared, found := doc.Find(instance)
	if !found || declared.Spec.Module == nil {
		return review, nil
	}

	review.InstalledVersion = declared.Spec.Module.Version
	if declared.Spec.Module.Asked != nil {
		review.NewAsks = beyondAsks(asked, *declared.Spec.Module.Asked, namespace.String())
	}
	installed, installedErr := manifest.ParseVersion(review.InstalledVersion)
	bundled, bundledErr := manifest.ParseVersion(review.Version)
	review.Downgrade = installedErr == nil && bundledErr == nil && bundled.Compare(installed) < 0

	return review, nil
}

// activateBundledExtensions installs every release of the upgrade's module
// plan after the backup and before the core swap, in the plan's order: the
// signed bundle's extensions and the releases the plan took from the index.
// No process is running at this point. The candidate core starts all
// installed releases together during its trial.
func activateBundledExtensions(
	ctx context.Context,
	core config.Core,
	configPath string,
	journal upgrade.Journal,
) error {
	for _, extension := range journal.Extensions {
		shown, err := shownByReview(journal, extension.Module)
		if err != nil {
			return err
		}
		_, err = installDeclared(ctx, core, configPath, moduleInstall{
			request: moduleRequest{
				source:     extension.Module + versionSeparator + extension.Version,
				from:       extension.StagedPath,
				approve:    true,
				shown:      map[string]*install.ModuleAsks{shownByRequest: shown},
				address:    extension.Source,
				digest:     extension.SHA256,
				constraint: extension.Constraint,
				pulled:     extension.Pulled,
			},
			grants:        func() (*moduleAuthority, func(), error) { return openModuleGrants(ctx, core) },
			candidateCore: journal.ToVersion,
		})
		if err != nil {
			return fmt.Errorf("install bundled extension %s@%s: %w",
				extension.Module, extension.Version, err)
		}
	}

	if len(journal.Extensions) == 0 {
		return nil
	}

	// The lock follows the declarations it names, and the backup holds the
	// lock, so a rollback restores both together.
	return recordLock(core, configPath)
}

// shownByReview is the ceiling an apply grants one bundled extension: what
// preparation showed for it. An extension with no review, or one whose review
// needed approval the operator did not give, is refused.
func shownByReview(journal upgrade.Journal, module string) (*install.ModuleAsks, error) {
	index := slices.IndexFunc(journal.ExtensionReviews, func(review upgrade.ExtensionReview) bool {
		return review.Module == module
	})
	if index < 0 {
		return nil, fmt.Errorf("%w: bundled extension %s has no review from preparation", upgrade.ErrPreflight, module)
	}
	review := journal.ExtensionReviews[index]
	if review.NeedsApproval() && !journal.ExtensionsApproved {
		return nil, fmt.Errorf("%w: bundled extension %s needs the operator's approval", upgrade.ErrPreflight, module)
	}

	var shown install.ModuleAsks
	if err := json.Unmarshal(review.Shown, &shown); err != nil {
		return nil, fmt.Errorf("%w: read what was shown for %s: %w", upgrade.ErrPreflight, module, err)
	}

	return &shown, nil
}
