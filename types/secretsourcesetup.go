package types

type (
	// SecretSourceSetupTarget is where setup writes in Infisical.
	SecretSourceSetupTarget struct {
		Mode string `json:"mode,omitempty"`
		// ProjectName names the project to create (new-project).
		ProjectName string `json:"projectName,omitempty"`
		// ProjectID selects an existing project (existing-project, shared-folder).
		ProjectID string `json:"projectId,omitempty"`
		// Environment is the environment slug, such as prod.
		Environment string `json:"environment,omitempty"`
		// SecretPath is the folder path. It is created when missing.
		SecretPath string `json:"secretPath,omitempty"`
		// FolderName names the Bitwarden folder to create (new-folder).
		FolderName string `json:"folderName,omitempty"`
		// FolderID selects an existing Bitwarden folder (existing-folder).
		FolderID string `json:"folderId,omitempty"`
		// Mount and KVVersion select the Vault or OpenBao KV mount (kv-path);
		// SecretPath is the secret's path in it.
		Mount     string `json:"mount,omitempty"`
		KVVersion int    `json:"kvVersion,omitempty"`
	}

	SecretSourceSetupPlanRequest struct {
		SourceID string `json:"sourceId"`
		// Target is optional: without it the plan lists the project's variables
		// only. Bitwarden targets have no environment, so neither field is
		// required by the schema; the provider validates the target instead.
		Target SecretSourceSetupTarget `json:"target,omitzero"`
	}

	// SecretSourceSetupVariable describes one variable the project uses. It never carries a
	// value.
	SecretSourceSetupVariable struct {
		Key string `json:"key"`
		// Remote is the key's state at the target: missing, same, different,
		// exists, or unknown.
		Remote string `json:"remote"`
		// InCompose is true when the compose files reference ${KEY}.
		InCompose bool `json:"inCompose"`
		// ComposeDefault is true when every reference has a default, such as
		// ${KEY:-value}, so compose works without it.
		ComposeDefault bool `json:"composeDefault"`
		// ComposeRequired is true when a reference fails without it, such as
		// ${KEY:?message}.
		ComposeRequired bool `json:"composeRequired"`
		// InEnvFile is true when the project's .env defines the key.
		InEnvFile bool `json:"inEnvFile"`
		// EnvEmpty is true when the .env defines the key with an empty value.
		EnvEmpty bool `json:"envEmpty"`
		// FromGit is true when the value comes from the Git repository's env
		// file, so it cannot be removed from the .env here.
		FromGit bool `json:"fromGit"`
		// SecretLike is true for names such as *_PASSWORD, *_TOKEN, or *_KEY.
		SecretLike bool `json:"secretLike"`
	}

	// SecretSourceSetupIdentity is a machine identity found in Infisical.
	SecretSourceSetupIdentity struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	// SecretSourceSetupPlan is what setup would do, computed without writing anything.
	SecretSourceSetupPlan struct {
		// DeployIdentity is the source's deploy identity, found by client ID, so
		// setup can give it read access to the new secrets.
		DeployIdentity *SecretSourceSetupIdentity `json:"deployIdentity,omitempty"`
		// Provider is the source's provider; it decides the available modes.
		Provider string `json:"provider"`
		// SuggestedProjectName and SuggestedSecretPath prefill the wizard.
		SuggestedProjectName string `json:"suggestedProjectName"`
		SuggestedSecretPath  string `json:"suggestedSecretPath"`
		SuggestedFolderName  string `json:"suggestedFolderName"`
		// RemoteError explains why the target could not be read.
		RemoteError         string                      `json:"remoteError,omitempty"`
		DeployIdentityError string                      `json:"deployIdentityError,omitempty"`
		Variables           []SecretSourceSetupVariable `json:"variables"`
		// CanWrite is false when the source has no setup identity.
		CanWrite bool `json:"canWrite"`
		// ProjectNameTaken is true when a project or folder with the requested
		// name already exists (new-project, new-folder), so the wizard can offer
		// to use it instead.
		ProjectNameTaken bool `json:"projectNameTaken"`
		// HasGitSource is true for projects synced from Git.
		HasGitSource bool `json:"hasGitSource"`
		// AlreadyBound is true when the project already has bindings. Setup
		// updates the binding to the same target, or adds one after them.
		AlreadyBound bool `json:"alreadyBound"`
	}

	SecretSourceSetupApplyRequest struct {
		SourceID string `json:"sourceId"`
		// Values is import or placeholder.
		Values string `json:"values"`
		// EnvFile is keep or remove. Removal happens only after the deploy
		// identity reads back every imported value unchanged.
		EnvFile string                  `json:"envFile"`
		Target  SecretSourceSetupTarget `json:"target"`
		// Keys are the variables to create in Infisical.
		Keys []string `json:"keys"`
		// OverwriteKeys replace values that already differ in Infisical. Other
		// existing keys keep their Infisical value.
		OverwriteKeys []string `json:"overwriteKeys,omitempty"`
		// KeepBackup writes the previous .env next to it before removal.
		KeepBackup bool `json:"keepBackup"`
		// GrantDeployIdentity gives the deploy identity the viewer role on the
		// project.
		GrantDeployIdentity bool `json:"grantDeployIdentity"`
		Required            bool `json:"required"`
		AutoRedeploy        bool `json:"autoRedeploy"`
	}

	// SecretSourceSetupStep is one step of an applied setup, in order.
	SecretSourceSetupStep struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}

	SecretSourceSetupResult struct {
		SecretSourceBinding *SecretSourceBinding `json:"binding,omitempty"`
		// BackupFile is the .env backup, relative to the project directory.
		BackupFile string                  `json:"backupFile,omitempty"`
		Steps      []SecretSourceSetupStep `json:"steps"`
		// RemovedKeys were removed from the .env.
		RemovedKeys []string `json:"removedKeys,omitempty"`
		// OK is true when every step is done or skipped.
		OK bool `json:"ok"`
	}
)

const (
	// Project setup: moves a project's variables into a secret manager and binds
	// the project in one step. Infisical writes with the source's setup identity;
	// Bitwarden writes through bw serve into a folder of the signed-in account.
	// The binding it creates reads the usual way.

	// Where setup puts the project's secrets.
	// SecretSourceSetupModeNewProject creates an Infisical project named after the Arcane
	// project and uses the root path of one of its environments.
	SecretSourceSetupModeNewProject = "new-project"
	// SecretSourceSetupModeExistingProject uses a path of a project that already exists.
	SecretSourceSetupModeExistingProject = "existing-project"
	// SecretSourceSetupModeSharedFolder uses a folder per Arcane project inside a shared
	// project, such as /immich in a "homelab" project.
	SecretSourceSetupModeSharedFolder = "shared-folder"
	// SecretSourceSetupModeNewFolder creates a Bitwarden folder named after the Arcane
	// project, with one secure note per variable.
	SecretSourceSetupModeNewFolder = "new-folder"
	// SecretSourceSetupModeExistingFolder uses a Bitwarden folder that already exists.
	SecretSourceSetupModeExistingFolder = "existing-folder"
	// SecretSourceSetupModeKVPath writes one Vault or OpenBao secret at a KV path,
	// creating it when missing and adding to it when present.
	SecretSourceSetupModeKVPath = "kv-path"

	// How setup fills the secrets it creates.
	// SecretSourceSetupValuesImport copies the values from the project's .env.
	SecretSourceSetupValuesImport = "import"
	// SecretSourceSetupValuesPlaceholder creates the keys with empty values to fill in
	// Infisical.
	SecretSourceSetupValuesPlaceholder = "placeholder"

	// What setup does with the imported keys in the project's .env.
	SecretSourceSetupEnvFileKeep   = "keep"
	SecretSourceSetupEnvFileRemove = "remove"

	// Where a variable stands in Infisical before setup.
	SecretSourceSetupRemoteMissing   = "missing"
	SecretSourceSetupRemoteSame      = "same"
	SecretSourceSetupRemoteDifferent = "different"
	// SecretSourceSetupRemoteExists means Infisical has the key and the .env does not.
	SecretSourceSetupRemoteExists = "exists"
	// SecretSourceSetupRemoteUnknown means the target could not be read.
	SecretSourceSetupRemoteUnknown = "unknown"

	// Outcome of one setup step.
	SecretSourceSetupStepDone    = "done"
	SecretSourceSetupStepSkipped = "skipped"
	SecretSourceSetupStepFailed  = "failed"
	// SecretSourceSetupStepPending means Infisical holds the change for approval.
	SecretSourceSetupStepPending = "pending"
)
