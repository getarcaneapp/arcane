import { PersistedState } from 'runed';
import settingsStore from './config-store';

export type DeployPullPolicy = 'missing' | 'always' | 'never';

export type DeployOptionsState = {
	pullPolicy: DeployPullPolicy;
	forceRecreate: boolean;
	removeOrphans: boolean;
	recreateVolumes: boolean;
};

// recreateVolumes destroys volume data, so it is deliberately left out of the
// persisted options: it must be opted into again after every reload rather than
// silently carrying over to later deployments.
type PersistedDeployOptions = Omit<DeployOptionsState, 'recreateVolumes'>;

const defaultDeployOptions: DeployOptionsState = {
	pullPolicy: 'missing',
	forceRecreate: false,
	removeOrphans: false,
	recreateVolumes: false
};

const persistedOptions = new PersistedState<PersistedDeployOptions>('arcane-deploy-options', {
	pullPolicy: defaultDeployOptions.pullPolicy,
	forceRecreate: defaultDeployOptions.forceRecreate,
	removeOrphans: defaultDeployOptions.removeOrphans
});
const userOverrodePullPolicy = new PersistedState<boolean>('arcane-deploy-options-user-overrode-pull-policy', false);

function isDeployPullPolicy(value: unknown): value is DeployPullPolicy {
	return value === 'missing' || value === 'always' || value === 'never';
}

const persistedCurrent = persistedOptions.current;

let state = $state<DeployOptionsState>({
	pullPolicy: isDeployPullPolicy(persistedCurrent?.pullPolicy) ? persistedCurrent.pullPolicy : defaultDeployOptions.pullPolicy,
	forceRecreate: persistedCurrent?.forceRecreate === true,
	removeOrphans: persistedCurrent?.removeOrphans === true,
	recreateVolumes: defaultDeployOptions.recreateVolumes
});

function persistState() {
	persistedOptions.current = {
		pullPolicy: state.pullPolicy,
		forceRecreate: state.forceRecreate,
		removeOrphans: state.removeOrphans
	};
}

settingsStore.subscribe((settings) => {
	if (!settings || userOverrodePullPolicy.current) {
		return;
	}

	if (isDeployPullPolicy(settings.defaultDeployPullPolicy)) {
		state.pullPolicy = settings.defaultDeployPullPolicy;
		persistState();
	}
});

export const deployOptionsStore = {
	get current(): DeployOptionsState {
		return state;
	},
	get pullPolicy(): DeployPullPolicy {
		return state.pullPolicy;
	},
	get forceRecreate(): boolean {
		return state.forceRecreate;
	},
	get removeOrphans(): boolean {
		return state.removeOrphans;
	},
	get recreateVolumes(): boolean {
		return state.recreateVolumes;
	},
	setPullPolicy(value: DeployPullPolicy) {
		if (!isDeployPullPolicy(value)) {
			return;
		}

		state.pullPolicy = value;
		userOverrodePullPolicy.current = true;
		persistState();
	},
	setForceRecreate(value: boolean) {
		state.forceRecreate = value;
		persistState();
	},
	setRemoveOrphans(value: boolean) {
		state.removeOrphans = value;
		persistState();
	},
	setRecreateVolumes(value: boolean) {
		state.recreateVolumes = value;
	},
	toggleForceRecreate() {
		state.forceRecreate = !state.forceRecreate;
		persistState();
	},
	// Consuming read: each deployment spends the recreateVolumes opt-in, so a
	// later deployment cannot silently reuse the destructive consent. Callers
	// deploying a batch must take one snapshot for the whole batch.
	takeRequestOptions(): DeployOptionsState {
		const options = {
			pullPolicy: state.pullPolicy,
			forceRecreate: state.forceRecreate,
			removeOrphans: state.removeOrphans,
			recreateVolumes: state.recreateVolumes
		};
		state.recreateVolumes = false;
		return options;
	}
};
