<script lang="ts">
  import type { WineSetup } from "./wine.svelte";

  // Apple Silicon Macs run the game's Wine through Rosetta 2, which macOS
  // installs only when asked.
  let { wine }: { wine: WineSetup } = $props();
</script>

<section class="mt-6 max-w-xl rounded border border-line bg-panel/90 p-5">
  <h2 class="title text-[14px]">Rosetta 2 is needed</h2>
  <p class="mt-2 text-[13px] leading-relaxed text-body">
    Diablo II is a Windows game. On this Mac it runs through Wine, which needs Apple's Rosetta 2 to run on Apple silicon. macOS
    will ask for your password to install it.
  </p>
  <button
    class="title mt-4 rounded-[3px] bg-accent px-6 py-2 text-[13px] !text-white hover:brightness-125 disabled:opacity-50"
    disabled={wine.installingRosetta}
    onclick={() => wine.installRosetta()}
  >
    {wine.installingRosetta ? "Installing Rosetta…" : "Install Rosetta"}
  </button>
</section>
