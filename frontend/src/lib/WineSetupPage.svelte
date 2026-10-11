<script lang="ts">
  import { onMount } from "svelte";
  import RosettaPrompt from "./RosettaPrompt.svelte";
  import StepList from "./StepList.svelte";
  import { app } from "./state.svelte";
  import { WineSetup } from "./wine.svelte";

  // Shown when Play finds Wine isn't ready, such as on a Mac whose first
  // run used an install it already had, or after Wine was updated.
  const wine = new WineSetup();

  async function run() {
    if (await wine.run()) await app.wineReady();
  }

  onMount(run);
</script>

<div class="h-full overflow-y-auto px-10 pt-8 pb-10">
  <div class="mx-auto max-w-3xl">
    <h1 class="title text-[26px]">Setting up Wine</h1>
    <p class="mt-3 max-w-2xl text-[13px] leading-relaxed text-body">
      Diablo II is a Windows game, so on this computer it runs through Wine. The launcher sets it up once, in a folder of its own.
    </p>

    <StepList rows={wine.rows} />

    {#if wine.needsRosetta}
      <RosettaPrompt {wine} onReady={() => app.wineReady()} />
    {/if}
    {#if wine.error}
      <p class="mt-4 max-w-xl whitespace-pre-line text-[12px] text-bad">{wine.error}</p>
    {/if}

    <div class="mt-8 flex items-center gap-4">
      {#if wine.running}
        <button class="title rounded-[3px] border border-edge px-6 py-2 text-[13px] hover:border-muted" onclick={() => wine.cancel()}>Cancel</button>
      {:else if !wine.done}
        {#if !wine.needsRosetta}
          <button class="title rounded-[3px] bg-accent px-6 py-2 text-[13px] !text-white hover:brightness-125" onclick={run}>Try again</button>
        {/if}
        <button class="title text-[12px] !text-muted hover:!text-title" onclick={() => app.leaveWineSetup()}>Back</button>
      {/if}
    </div>
  </div>
</div>
