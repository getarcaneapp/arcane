<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { CopyIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { systemService } from '$lib/services/system-service.js';
	import { handleApiResultWithCallbacks, tryCatch } from '$lib/utils/api';
	import { tick } from 'svelte';
	import { toast } from 'svelte-sonner';

	type ConvertedDockerRun = {
		dockerCompose: string;
		envVars: string;
		serviceName: string;
	};

	let {
		open = $bindable(false),
		converting = $bindable(false),
		onConverted
	}: {
		open: boolean;
		converting: boolean;
		onConverted: (data: ConvertedDockerRun) => void;
	} = $props();

	let dockerRunCommand = $state('');
	const exampleCommands = [m.compose_example_command_1(), m.compose_example_command_2(), m.compose_example_command_3()];

	function useExample(command: string) {
		dockerRunCommand = command;
	}

	async function handleConvertDockerRun() {
		if (!dockerRunCommand.trim()) {
			toast.error(m.compose_enter_docker_run_command());
			return;
		}

		handleApiResultWithCallbacks({
			result: await tryCatch(systemService.convert(dockerRunCommand)),
			message: m.compose_convert_failed(),
			setLoadingState: (value) => (converting = value),
			onSuccess: async (data) => {
				onConverted(data);
				await tick();
				toast.success(m.compose_convert_success());
				dockerRunCommand = '';
				open = false;
			}
		});
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[80vh] sm:max-w-[800px]">
		<Dialog.Header>
			<Dialog.Title>{m.compose_converter_title()}</Dialog.Title>
			<Dialog.Description>{m.compose_converter_description()}</Dialog.Description>
		</Dialog.Header>

		<div class="max-h-[60vh] space-y-4 overflow-y-auto">
			<div class="space-y-2">
				<Label for="dockerRunCommand">{m.compose_docker_run_command_label()}</Label>
				<Textarea
					id="dockerRunCommand"
					bind:value={dockerRunCommand}
					placeholder={m.compose_docker_run_placeholder()}
					rows={3}
					disabled={converting}
					class="font-mono text-sm"
				/>
			</div>

			<div class="space-y-2">
				<Label class="text-muted-foreground text-xs">{m.compose_example_commands_label()}</Label>
				<div class="space-y-1">
					{#each exampleCommands as command (command)}
						<ArcaneButton
							action="base"
							tone="ghost"
							size="sm"
							class="h-auto w-full justify-start p-2 text-left font-mono text-xs break-all whitespace-normal"
							onclick={() => useExample(command)}
							icon={CopyIcon}
							customLabel={command}
						/>
					{/each}
				</div>
			</div>
		</div>

		<div class="flex w-full justify-end pt-4">
			<ArcaneButton
				action="create"
				disabled={!dockerRunCommand.trim() || converting}
				onclick={handleConvertDockerRun}
				loading={converting}
				customLabel={m.compose_convert_action()}
				loadingLabel={m.compose_converting()}
			/>
		</div>
	</Dialog.Content>
</Dialog.Root>
