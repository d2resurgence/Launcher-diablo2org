<script lang="ts">
  import { SupportService } from "../../bindings/github.com/diablo2org/launcher/internal/app";

  // Beside a set-up failure: the launcher's logs say what went wrong, and on
  // the welcome pages Settings, where they otherwise live, isn't reachable.
</script>

<button class="mt-1.5 block text-[12px] text-gold underline decoration-gold-line underline-offset-2 hover:text-[#fff1c8]" onclick={() => SupportService.OpenLogs()}>
  Open the launcher's logs
</button>
