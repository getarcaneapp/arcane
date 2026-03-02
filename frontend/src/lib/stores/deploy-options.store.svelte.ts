import { PersistedState } from 'runed';
import settingsStore from './config-store';

export type DeployPullPolicy = 'missing' | 'always' | 'never';

export type DeployOptionsState = {
	pullPolicy: DeployPullPolicy;
	forceRecreate: boolean;
};

const defaultDeployOptions: DeployOptionsState = {
	pullPolicy: 'missing',
	forceRecreate: false
};

const persistedOptions = new PersistedState<DeployOptionsState>('arcane-deploy-options', defaultDeployOptions);
const userOverrodePullPolicy = new PersistedState<boolean>('arcane-deploy-options-user-overrode-pull-policy', false);

function isDeployPullPolicy(value: unknown): value is DeployPullPolicy {
	return value === 'missing' || value === 'always' || value === 'never';
}

const persistedCurrent = persistedOptions.current;

let state = $state<DeployOptionsState>({
	pullPolicy: isDeployPullPolicy(persistedCurrent?.pullPolicy) ? persistedCurrent.pullPolicy : defaultDeployOptions.pullPolicy,
	forceRecreate: persistedCurrent?.forceRecreate === true
});

function persistState() {
	persistedOptions.current = {
		pullPolicy: state.pullPolicy,
		forceRecreate: state.forceRecreate
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
	toggleForceRecreate() {
		state.forceRecreate = !state.forceRecreate;
		persistState();
	},
	getRequestOptions(): DeployOptionsState {
		return {
			pullPolicy: state.pullPolicy,
			forceRecreate: state.forceRecreate
		};
	}
};
