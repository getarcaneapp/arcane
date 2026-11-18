package secretsource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/secretsource/children/bitwarden"
	"getarcane.dev/arcane/v3/internal/secretsource/children/infisical"
	"getarcane.dev/arcane/v3/internal/secretsource/children/vault"
	infisicalclient "getarcane.dev/arcane/v3/pkg/infisical"
	"getarcane.dev/types/v3"
)

type (
	// setupWriterInternal is one provider's side of project setup. A writer is
	// created per plan or apply and may keep state between its calls.
	setupWriterInternal interface {
		NormalizeTarget(target types.SecretSourceSetupTarget, arcaneProjectName string) (types.SecretSourceSetupTarget, error)
		// CreatesContainer reports whether the target creates a project or folder.
		CreatesContainer(target types.SecretSourceSetupTarget) bool
		// NameTaken reports whether the project or folder a target would create
		// already exists.
		NameTaken(ctx context.Context, target types.SecretSourceSetupTarget) (bool, error)
		// Prepare creates what the target needs and returns it with IDs filled in,
		// plus a step per thing it did. On error the steps are those completed.
		Prepare(ctx context.Context, target types.SecretSourceSetupTarget) (types.SecretSourceSetupTarget, []types.SecretSourceSetupStep, error)
		// RemoteValues returns the values already at the target, by key.
		RemoteValues(ctx context.Context, target types.SecretSourceSetupTarget) (map[string]string, error)
		// Write creates the create keys and replaces the overwrite keys with their
		// values. pending reports a change held for approval.
		Write(ctx context.Context, target types.SecretSourceSetupTarget, values map[string]string, create, overwrite []string) (pending bool, err error)
		// DeployIdentity finds the identity deploys read with, or nil when the
		// provider has no separate one.
		DeployIdentity(ctx context.Context) (*types.SecretSourceSetupIdentity, error)
		// Grant gives the deploy identity read access to the target.
		Grant(ctx context.Context, target types.SecretSourceSetupTarget) types.SecretSourceSetupStep
		BindingTarget(target types.SecretSourceSetupTarget) types.SecretSourceBindingTarget
	}

	// ---- Infisical ----
	infisicalWriterInternal struct {
		setup *infisical.Setup
	}

	// ---- Bitwarden ----
	bitwardenWriterInternal struct {
		setup *bitwarden.Setup
		// items are the target folder's items from the last RemoteValues call;
		// updates need their IDs.
		items map[string]bitwarden.RemoteItem
	}

	// ---- Vault / OpenBao ----
	vaultWriterInternal struct {
		setup *vault.Setup
	}

	// ---- Steps ----

	// stepErrorInternal is a failure of a named step.
	stepErrorInternal struct {
		err  error
		step string
	}
)

// newSetupWriterInternal returns the source's writer, or nil when the source
// cannot write (an Infisical source without a setup identity).
func newSetupWriterInternal(httpClient *http.Client, source *SecretSource) (setupWriterInternal, error) {
	switch source.Provider {
	case types.SecretSourceProviderInfisical:
		if source.Settings.Infisical == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("infisical settings are required"))
		}
		setupSecret, err := decryptCredentialInternal(source.SetupCredential)
		if err != nil {
			return nil, err
		}
		setup, err := infisical.NewSetup(httpClient, *source.Settings.Infisical, setupSecret)
		if errors.Is(err, infisical.ErrNoSetupIdentity) {
			return nil, nil
		}
		if err != nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, err)
		}
		return &infisicalWriterInternal{setup: setup}, nil
	case types.SecretSourceProviderBitwarden:
		if source.Settings.Bitwarden == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("bitwarden settings are required"))
		}
		setup, err := bitwarden.NewSetup(httpClient, *source.Settings.Bitwarden)
		if err != nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, err)
		}
		return &bitwardenWriterInternal{setup: setup}, nil
	case types.SecretSourceProviderVault:
		if source.Settings.Vault == nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, errors.New("vault settings are required"))
		}
		setupToken, err := decryptCredentialInternal(source.SetupCredential)
		if err != nil {
			return nil, err
		}
		setup, err := vault.NewSetup(httpClient, *source.Settings.Vault, setupToken)
		if errors.Is(err, vault.ErrNoSetupToken) {
			return nil, nil
		}
		if err != nil {
			return nil, common.Classify(common.ErrSecretSourceInvalid, err)
		}
		return &vaultWriterInternal{setup: setup}, nil
	case types.SecretSourceProviderDoppler, types.SecretSourceProviderOnePassword,
		types.SecretSourceProviderProtonPass, types.SecretSourceProviderHTTP:
		// Read-only providers: setup lists the variables, and binding is manual.
		return nil, nil
	default:
		return nil, common.Classify(common.ErrSecretSourceInvalid, fmt.Errorf("unknown provider %q", source.Provider))
	}
}

func (w *infisicalWriterInternal) NormalizeTarget(target types.SecretSourceSetupTarget, arcaneProjectName string) (types.SecretSourceSetupTarget, error) {
	return infisical.NormalizeSetupTarget(target, arcaneProjectName)
}

func (w *infisicalWriterInternal) CreatesContainer(target types.SecretSourceSetupTarget) bool {
	return target.Mode == types.SecretSourceSetupModeNewProject
}

func (w *infisicalWriterInternal) NameTaken(ctx context.Context, target types.SecretSourceSetupTarget) (bool, error) {
	return w.setup.ProjectNameTaken(ctx, target.ProjectName)
}

func (w *infisicalWriterInternal) Prepare(ctx context.Context, target types.SecretSourceSetupTarget) (types.SecretSourceSetupTarget, []types.SecretSourceSetupStep, error) {
	steps := []types.SecretSourceSetupStep{}
	if target.Mode == types.SecretSourceSetupModeNewProject {
		projectID, err := w.setup.CreateProject(ctx, target.ProjectName, target.Environment)
		if err != nil {
			return target, steps, stepErrorInternal{step: setupStepProject, err: err}
		}
		target.ProjectID = projectID
		steps = append(steps, doneStepInternal(setupStepProject, fmt.Sprintf("Created project %q (ID %s)", target.ProjectName, projectID)))
	} else {
		steps = append(steps, skippedStepInternal(setupStepProject, "Using the existing project"))
	}

	if target.SecretPath == "/" {
		return target, append(steps, skippedStepInternal(setupStepFolder, "Using the root path")), nil
	}
	created, err := w.setup.EnsurePath(ctx, target.ProjectID, target.Environment, target.SecretPath)
	if err != nil {
		return target, steps, stepErrorInternal{step: setupStepFolder, err: err}
	}
	if len(created) == 0 {
		return target, append(steps, skippedStepInternal(setupStepFolder, target.SecretPath+" already exists")), nil
	}
	return target, append(steps, doneStepInternal(setupStepFolder, "Created "+strings.Join(created, ", "))), nil
}

func (w *infisicalWriterInternal) RemoteValues(ctx context.Context, target types.SecretSourceSetupTarget) (map[string]string, error) {
	return w.setup.RemoteValues(ctx, target)
}

func (w *infisicalWriterInternal) Write(ctx context.Context, target types.SecretSourceSetupTarget, values map[string]string, create, overwrite []string) (bool, error) {
	toInput := func(keys []string, comment string) []infisicalclient.SecretInput {
		inputs := make([]infisicalclient.SecretInput, 0, len(keys))
		for _, key := range keys {
			inputs = append(inputs, infisicalclient.SecretInput{Key: key, Value: values[key], Comment: comment})
		}
		return inputs
	}
	return w.setup.WriteSecrets(ctx, target.ProjectID, target.Environment, target.SecretPath, toInput(create, "Created by Arcane"), toInput(overwrite, ""))
}

func (w *infisicalWriterInternal) DeployIdentity(ctx context.Context) (*types.SecretSourceSetupIdentity, error) {
	return w.setup.FindDeployIdentity(ctx)
}

func (w *infisicalWriterInternal) Grant(ctx context.Context, target types.SecretSourceSetupTarget) types.SecretSourceSetupStep {
	identity, err := w.setup.FindDeployIdentity(ctx)
	if err != nil {
		return failedStepInternal(setupStepGrant, err.Error())
	}
	if identity == nil {
		return failedStepInternal(setupStepGrant, "could not find the deploy identity; add it to the Infisical project with the viewer role")
	}
	already, err := w.setup.GrantDeployIdentity(ctx, target.ProjectID, identity.ID)
	if err != nil {
		return failedStepInternal(setupStepGrant, err.Error())
	}
	if already {
		return skippedStepInternal(setupStepGrant, identity.Name+" already has access")
	}
	return doneStepInternal(setupStepGrant, "Gave "+identity.Name+" the "+infisical.DeployRole+" role")
}

func (w *infisicalWriterInternal) BindingTarget(target types.SecretSourceSetupTarget) types.SecretSourceBindingTarget {
	return types.SecretSourceBindingTarget{Infisical: &types.SecretSourceInfisicalTarget{
		ProjectID:        target.ProjectID,
		Environment:      target.Environment,
		SecretPath:       target.SecretPath,
		IncludeImports:   true,
		ExpandReferences: true,
	}}
}

func (w *bitwardenWriterInternal) NormalizeTarget(target types.SecretSourceSetupTarget, arcaneProjectName string) (types.SecretSourceSetupTarget, error) {
	return bitwarden.NormalizeSetupTarget(target, arcaneProjectName)
}

func (w *bitwardenWriterInternal) CreatesContainer(target types.SecretSourceSetupTarget) bool {
	return target.Mode == types.SecretSourceSetupModeNewFolder
}

func (w *bitwardenWriterInternal) NameTaken(ctx context.Context, target types.SecretSourceSetupTarget) (bool, error) {
	return w.setup.FolderNameTaken(ctx, target.FolderName)
}

func (w *bitwardenWriterInternal) Prepare(ctx context.Context, target types.SecretSourceSetupTarget) (types.SecretSourceSetupTarget, []types.SecretSourceSetupStep, error) {
	if target.Mode != types.SecretSourceSetupModeNewFolder {
		return target, []types.SecretSourceSetupStep{skippedStepInternal(setupStepFolder, "Using the existing folder")}, nil
	}
	folderID, err := w.setup.CreateFolder(ctx, target.FolderName)
	if err != nil {
		return target, nil, stepErrorInternal{step: setupStepFolder, err: err}
	}
	target.FolderID = folderID
	return target, []types.SecretSourceSetupStep{doneStepInternal(setupStepFolder, fmt.Sprintf("Created folder %q", target.FolderName))}, nil
}

func (w *bitwardenWriterInternal) RemoteValues(ctx context.Context, target types.SecretSourceSetupTarget) (map[string]string, error) {
	items, err := w.setup.RemoteItems(ctx, target.FolderID)
	if err != nil {
		return nil, err
	}
	w.items = items
	values := make(map[string]string, len(items))
	for name, item := range items {
		values[name] = item.Value
	}
	return values, nil
}

func (w *bitwardenWriterInternal) Write(ctx context.Context, target types.SecretSourceSetupTarget, values map[string]string, create, overwrite []string) (bool, error) {
	if err := w.setup.CreateItems(ctx, target.FolderID, values, create); err != nil {
		return false, err
	}
	itemIDs := make(map[string]string, len(overwrite))
	for _, key := range overwrite {
		item, ok := w.items[key]
		if !ok {
			return false, fmt.Errorf("update %s: the item is no longer in the folder", key)
		}
		itemIDs[key] = item.ID
	}
	return false, w.setup.UpdateItems(ctx, values, itemIDs, overwrite)
}

func (w *bitwardenWriterInternal) DeployIdentity(context.Context) (*types.SecretSourceSetupIdentity, error) {
	return nil, nil
}

func (w *bitwardenWriterInternal) Grant(context.Context, types.SecretSourceSetupTarget) types.SecretSourceSetupStep {
	return skippedStepInternal(setupStepGrant, "Not needed: bw serve reads with the account that owns the folder")
}

func (w *bitwardenWriterInternal) BindingTarget(target types.SecretSourceSetupTarget) types.SecretSourceBindingTarget {
	return types.SecretSourceBindingTarget{Bitwarden: &types.SecretSourceBitwardenTarget{
		Scope: types.SecretSourceBitwardenScopeFolder,
		ID:    target.FolderID,
		Name:  target.FolderName,
	}}
}

func (w *vaultWriterInternal) NormalizeTarget(target types.SecretSourceSetupTarget, arcaneProjectName string) (types.SecretSourceSetupTarget, error) {
	return vault.NormalizeSetupTarget(target, arcaneProjectName)
}

// CreatesContainer is false: a KV path needs no container, and writing to an
// existing secret adds to it.
func (w *vaultWriterInternal) CreatesContainer(types.SecretSourceSetupTarget) bool {
	return false
}

func (w *vaultWriterInternal) NameTaken(context.Context, types.SecretSourceSetupTarget) (bool, error) {
	return false, nil
}

func (w *vaultWriterInternal) Prepare(_ context.Context, target types.SecretSourceSetupTarget) (types.SecretSourceSetupTarget, []types.SecretSourceSetupStep, error) {
	return target, []types.SecretSourceSetupStep{skippedStepInternal(setupStepFolder, "Not needed: the secret is created at "+target.Mount+"/"+target.SecretPath)}, nil
}

func (w *vaultWriterInternal) RemoteValues(ctx context.Context, target types.SecretSourceSetupTarget) (map[string]string, error) {
	return w.setup.RemoteValues(ctx, target)
}

func (w *vaultWriterInternal) Write(ctx context.Context, target types.SecretSourceSetupTarget, values map[string]string, create, overwrite []string) (bool, error) {
	return false, w.setup.Write(ctx, target, values, create, overwrite)
}

func (w *vaultWriterInternal) DeployIdentity(context.Context) (*types.SecretSourceSetupIdentity, error) {
	return nil, nil
}

func (w *vaultWriterInternal) Grant(context.Context, types.SecretSourceSetupTarget) types.SecretSourceSetupStep {
	return skippedStepInternal(setupStepGrant, "Policies are managed in Vault/OpenBao; the deploy token needs read on this path")
}

func (w *vaultWriterInternal) BindingTarget(target types.SecretSourceSetupTarget) types.SecretSourceBindingTarget {
	return types.SecretSourceBindingTarget{Vault: vault.BindingTarget(target)}
}

func (e stepErrorInternal) Error() string { return e.err.Error() }

func (e stepErrorInternal) Unwrap() error { return e.err }

func doneStepInternal(id, detail string) types.SecretSourceSetupStep {
	return types.SecretSourceSetupStep{ID: id, Status: types.SecretSourceSetupStepDone, Detail: detail}
}

func skippedStepInternal(id, detail string) types.SecretSourceSetupStep {
	return types.SecretSourceSetupStep{ID: id, Status: types.SecretSourceSetupStepSkipped, Detail: detail}
}

func failedStepInternal(id, detail string) types.SecretSourceSetupStep {
	return types.SecretSourceSetupStep{ID: id, Status: types.SecretSourceSetupStepFailed, Detail: detail}
}
