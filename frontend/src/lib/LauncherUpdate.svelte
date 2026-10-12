<script lang="ts">
  import { Browser } from "@wailsio/runtime";
  import { bytes } from "./format";
  import { app } from "./state.svelte";
</script>

<!-- A newer launcher: its download, or the way to get it. -->
{#if app.update}
  {@const u = app.launcherUpdate}
  <div class="glass flex items-center gap-3 px-4 py-2 text-[12.5px]" role="status">
    {#if app.updateError}
      <span class="text-bad" title={app.updateError}>Update failed</span>
    {/if}
    {#if u}
      <span class="title text-[12px] !text-date">
        {u.total && u.done >= u.total ? "Starting installer…" : `Downloading ${app.update.version} · ${u.total ? Math.floor((u.done / u.total) * 100) : 0}%`}
      </span>
    {:else if app.update.installer}
      <span class="title text-[13px] !text-parchment">Launcher {app.update.version} is ready</span>
      <button
        class="underline decoration-gold-line underline-offset-2 text-gold hover:text-[#fff1c8]"
        title={`Downloads the ${app.update.version} installer (${bytes(app.update.installer.size)}), checks it, and runs it. The launcher closes while it installs.`}
        onclick={() => app.installUpdate()}
      >
        Update and restart
      </button>
      <button class="text-muted hover:text-gold" title="What's new" aria-label="What's new" onclick={() => Browser.OpenURL(app.update!.url)}>↗</button>
    {:else if app.update.download}
      <!-- macOS and Linux: the new package downloads in the browser, to install as the first. -->
      <span class="title text-[13px] !text-parchment">Launcher {app.update.version} is out</span>
      <button
        class="underline decoration-gold-line underline-offset-2 text-gold hover:text-[#fff1c8]"
        title={`Downloads ${app.update.version} in your browser. Install it as you did this one.`}
        onclick={() => Browser.OpenURL(app.update!.download)}
      >
        Download
      </button>
      <button class="text-muted hover:text-gold" title="What's new" aria-label="What's new" onclick={() => Browser.OpenURL(app.update!.url)}>↗</button>
    {:else}
      <button
        class="title text-[12px] !text-date hover:!text-title"
        title="A newer launcher is available. Opens the download page."
        onclick={() => Browser.OpenURL(app.update!.url)}
      >
        Launcher {app.update.version} available ↗
      </button>
    {/if}
  </div>
{/if}
