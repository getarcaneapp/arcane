<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import KeyValueEditor from '#lib/components/form/key-value-editor.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { AddIcon, CloseIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { SelectorKind } from '#lib/permissions.generated.js';
	import type { SelectorDraft } from '#lib/utils/rbac.js';

	let {
		draft = $bindable(),
		kinds,
		projectOptions = [],
		disabled = false
	}: {
		draft: SelectorDraft;
		kinds: readonly SelectorKind[];
		projectOptions?: string[];
		disabled?: boolean;
	} = $props();

	let projectInput = $state('');
	let nameInput = $state('');

	const projectSuggestions = $derived(projectOptions.filter((project) => !draft.projects.includes(project)));

	function addValue(list: 'projects' | 'names', raw: string): boolean {
		const value = raw.trim();
		if (!value || draft[list].includes(value)) return false;
		draft[list] = [...draft[list], value];
		return true;
	}

	function removeValue(list: 'projects' | 'names', value: string) {
		draft[list] = draft[list].filter((entry) => entry !== value);
	}

	function handleKeydown(event: KeyboardEvent, list: 'projects' | 'names') {
		if (event.key !== 'Enter') return;
		event.preventDefault();
		commit(list);
	}

	function commit(list: 'projects' | 'names') {
		if (list === 'projects') {
			addValue('projects', projectInput);
			projectInput = '';
		} else {
			addValue('names', nameInput);
			nameInput = '';
		}
	}
</script>

{#snippet chips(list: 'projects' | 'names')}
	{#if draft[list].length > 0}
		<div class="flex flex-wrap gap-2">
			{#each draft[list] as value (value)}
				<Badge variant="outline" mono>
					{value}
					<button
						type="button"
						class="text-muted-foreground hover:text-destructive"
						aria-label={m.common_remove()}
						{disabled}
						onclick={() => removeValue(list, value)}
					>
						<CloseIcon class="size-3" />
					</button>
				</Badge>
			{/each}
		</div>
	{/if}
{/snippet}

{#if kinds.length === 0}
	<p class="text-sm text-muted-foreground">{m.selector_unsupported()}</p>
{:else}
	<div class="space-y-4">
		{#if kinds.includes('labels')}
			<SettingsRow label={m.common_labels()} description={m.selector_labels_description()} layout="wide">
				<KeyValueEditor bind:rows={draft.labels} {disabled} addLabel={m.common_add()} />
			</SettingsRow>
		{/if}

		{#if kinds.includes('projects')}
			<SettingsRow
				for="selector-projects"
				label={m.selector_projects_label()}
				description={m.selector_projects_description()}
				layout="wide"
			>
				<div class="space-y-2">
					<div class="flex gap-2">
						<Input
							id="selector-projects"
							type="text"
							mono
							placeholder={m.selector_projects_placeholder()}
							bind:value={projectInput}
							{disabled}
							onkeydown={(event) => handleKeydown(event, 'projects')}
						/>
						<ArcaneButton
							action="base"
							tone="outline"
							size="icon"
							icon={AddIcon}
							showLabel={false}
							customLabel={m.common_add()}
							{disabled}
							onclick={() => commit('projects')}
						/>
					</div>
					{@render chips('projects')}
					{#if projectSuggestions.length > 0}
						<div class="flex flex-wrap items-center gap-2">
							<span class="text-xs text-muted-foreground">{m.selector_project_suggestions()}</span>
							{#each projectSuggestions as project (project)}
								<button type="button" {disabled} onclick={() => addValue('projects', project)}>
									<Badge variant="gray" mono>{project}</Badge>
								</button>
							{/each}
						</div>
					{/if}
				</div>
			</SettingsRow>
		{/if}

		{#if kinds.includes('owner')}
			<SettingsRow
				for="selector-owner"
				label={m.selector_owner_label()}
				description={m.selector_owner_description()}
				layout="switch"
			>
				<Switch id="selector-owner" bind:checked={draft.ownerOnly} {disabled} />
			</SettingsRow>
		{/if}

		{#if kinds.includes('names')}
			<SettingsRow
				for="selector-names"
				label={m.selector_names_label()}
				description={m.selector_names_description()}
				layout="wide"
			>
				<div class="space-y-2">
					<div class="flex gap-2">
						<Input
							id="selector-names"
							type="text"
							mono
							placeholder={m.selector_names_placeholder()}
							bind:value={nameInput}
							{disabled}
							onkeydown={(event) => handleKeydown(event, 'names')}
						/>
						<ArcaneButton
							action="base"
							tone="outline"
							size="icon"
							icon={AddIcon}
							showLabel={false}
							customLabel={m.common_add()}
							{disabled}
							onclick={() => commit('names')}
						/>
					</div>
					{@render chips('names')}
				</div>
			</SettingsRow>
		{/if}
	</div>
{/if}
