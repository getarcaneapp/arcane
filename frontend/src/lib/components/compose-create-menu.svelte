<!--
	compose-create-menu — the split-button + dropdown "create" menu shared by the
	new-project and new-swarm-stack compose pages. The primary button submits the
	form; the dropdown exposes template / docker-run / git-repo actions plus a
	"create template" item.

	Every label and action is passed in so this component owns no message keys and
	no business logic — it purely renders the shared chrome.
-->
<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { TerminalIcon, TemplateIcon, AddIcon, ArrowDownIcon as ChevronDown, GitBranchIcon } from '$lib/icons';
	import { dropdownContentClass, dropdownItemClass, templateBtnClass } from '$lib/utils/compose-flow';
	import { mergeProps } from 'bits-ui';

	interface Props {
		// Tooltip shows when the tooltipOpen prop is truthy-undefined (bits-ui
		// convention: `undefined` = auto/hover, `false` = force-closed).
		tooltipOpen: boolean | undefined;
		tooltipTitle: string;
		tooltipDescription: string;
		tooltipExample: string;
		// Show the tooltip body only when the name is still empty.
		tooltipVisible: boolean;

		// Primary create button.
		showCreateButton?: boolean;
		createDisabled: boolean;
		createLoading: boolean;
		createLabel: string;
		createLoadingLabel: string;
		onCreate: () => void;

		// Dropdown items.
		itemsDisabled: boolean;
		useTemplateLabel: string;
		onUseTemplate: () => void;
		convertLabel: string;
		onConvert: () => void;
		fromGitLabel: string;
		onFromGit: () => void | Promise<void>;

		// Create-template item.
		createTemplateLabel: string;
		createTemplateDisabled: boolean;
		createTemplateLoading: boolean;
		onCreateTemplate: () => void;
		// When set, the create-template item is gated behind this RBAC permission
		// (with a leading separator), matching the new-project page. When omitted,
		// the item renders unconditionally (new-swarm-stack page).
		createTemplatePermission?: string;
	}

	let {
		tooltipOpen,
		tooltipTitle,
		tooltipDescription,
		tooltipExample,
		tooltipVisible,
		showCreateButton = true,
		createDisabled,
		createLoading,
		createLabel,
		createLoadingLabel,
		onCreate,
		itemsDisabled,
		useTemplateLabel,
		onUseTemplate,
		convertLabel,
		onConvert,
		fromGitLabel,
		onFromGit,
		createTemplateLabel,
		createTemplateDisabled,
		createTemplateLoading,
		onCreateTemplate,
		createTemplatePermission
	}: Props = $props();
</script>

{#snippet createTemplateItem()}
	<DropdownMenu.Item class={dropdownItemClass} disabled={createTemplateDisabled} onclick={onCreateTemplate}>
		{#if createTemplateLoading}
			<Spinner class="size-4" />
		{:else}
			<AddIcon class="size-4" />
		{/if}
		{createTemplateLabel}
	</DropdownMenu.Item>
{/snippet}

<ButtonGroup.Root>
	{#if showCreateButton}
		<ArcaneTooltip.Root open={tooltipOpen}>
			<ArcaneTooltip.Trigger disabledChild={createDisabled || createLoading}>
				{#snippet child({ props })}
					{@const triggerProps = mergeProps(props, { onclick: onCreate })}
					<ArcaneButton
						{...triggerProps}
						action="create"
						tone="ghost"
						disabled={createDisabled}
						class={`${templateBtnClass} gap-2 rounded-r-none`}
						loading={createLoading}
						customLabel={createLabel}
						loadingLabel={createLoadingLabel}
					/>
				{/snippet}
			</ArcaneTooltip.Trigger>
			<ArcaneTooltip.Content class="arcane-tooltip-content max-w-[280px]">
				{#if tooltipVisible}
					<p class="mb-1 text-sm font-medium">{tooltipTitle}</p>
					<p class="text-muted-foreground text-xs">{tooltipDescription}</p>
					<p class="bg-muted mt-1.5 inline-block rounded px-1.5 py-0.5 font-mono text-xs">
						{tooltipExample}
					</p>
				{/if}
			</ArcaneTooltip.Content>
		</ArcaneTooltip.Root>
	{/if}

	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					class={`${templateBtnClass} -ml-px rounded-l-none px-2`}
					icon={ChevronDown}
				/>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end" class={dropdownContentClass}>
			<DropdownMenu.Group>
				<DropdownMenu.Item class={dropdownItemClass} disabled={itemsDisabled} onclick={onUseTemplate}>
					<TemplateIcon class="size-4" />
					{useTemplateLabel}
				</DropdownMenu.Item>
				<DropdownMenu.Item class={dropdownItemClass} onclick={onConvert}>
					<TerminalIcon class="size-4" />
					{convertLabel}
				</DropdownMenu.Item>
				<DropdownMenu.Item class={dropdownItemClass} onclick={onFromGit}>
					<GitBranchIcon class="size-4" />
					{fromGitLabel}
				</DropdownMenu.Item>
				{#if createTemplatePermission}
					<IfPermitted perm={createTemplatePermission}>
						<DropdownMenu.Separator />
						{@render createTemplateItem()}
					</IfPermitted>
				{:else}
					<DropdownMenu.Separator />
					{@render createTemplateItem()}
				{/if}
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
</ButtonGroup.Root>
