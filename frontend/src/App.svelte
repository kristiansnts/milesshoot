<script>
  import { onMount } from "svelte";
  import { initStores, status, config, hires } from "./lib/stores";
  import Unggah from "./screens/Unggah.svelte";
  import Pengaturan from "./screens/Pengaturan.svelte";
  import Placeholder from "./screens/Placeholder.svelte";
  import Ranking from "./screens/Ranking.svelte";
  import Hires from "./screens/Hires.svelte";
  import VideoFoto from "./screens/VideoFoto.svelte";
  import Log from "./screens/Log.svelte";

  let screen = "unggah";
  let settingsTab = "sumber";

  onMount(() => initStores());

  function toggleTheme() {
    const cur = document.documentElement.getAttribute("data-theme");
    const next = cur === "dark" ? "light" : "dark";
    document.documentElement.setAttribute("data-theme", next);
  }

  const nav = [
    { id: "unggah", label: "Unggah", grp: "Menu utama", icon: '<path d="M12 16V4M7 9l5-5 5 5"/><path d="M4 16v3a1 1 0 001 1h14a1 1 0 001-1v-3"/>' },
    { id: "hires", label: "Hi-res", icon: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 15l4-4 4 4 3-3 7 7"/>' },
    { id: "videofoto", label: "Video → Foto", icon: '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 9h18M3 15h18M8 5v14M16 5v14"/>' },
    { id: "ranking", label: "Ranking", icon: '<path d="M6 20V10M12 20V4M18 20v-7"/>' },
    { id: "pengaturan", label: "Pengaturan", grp: "Atur", icon: '<circle cx="12" cy="12" r="3"/><path d="M19 12a7 7 0 00-.1-1l2-1.5-2-3.4-2.3 1a7 7 0 00-1.7-1L16.5 2h-4l-.4 2.6a7 7 0 00-1.7 1l-2.3-1-2 3.4L5.1 11a7 7 0 000 2l-2 1.5 2 3.4 2.3-1a7 7 0 001.7 1l.4 2.6h4l.4-2.6a7 7 0 001.7-1l2.3 1 2-3.4-2-1.5a7 7 0 00.1-1z"/>' },
    { id: "log", label: "Log", icon: '<path d="M4 6h16M4 10h16M4 14h10M4 18h7"/>' },
  ];

  $: st = $status;
  $: cfg = $config;
  $: acct = st && st.account;
  $: fyOn = acct && acct.loggedIn;
  $: ftpOn = cfg && cfg.ftp && cfg.ftp.enabled;
  $: avatar = acct && acct.username ? acct.username[0].toUpperCase() : "?";
  // badge merah = pesanan hi-res yang belum terkirim
  $: hiresPending = $hires ? ($hires.exact || 0) + ($hires.byName || 0) + ($hires.none || 0) : 0;
</script>

<div class="app">
  <aside class="sidebar">
    <div class="brand"><div class="mark">W</div><b>milesshoot</b></div>
    {#each nav as n}
      {#if n.grp}<div class="grp">{n.grp}</div>{/if}
      <button class="nav" class:on={screen === n.id} on:click={() => (screen = n.id)}>
        <svg class="ico" viewBox="0 0 24 24">{@html n.icon}</svg>
        {n.label}
        {#if n.id === "hires" && hiresPending}<span class="bdg red">{hiresPending}</span>
        {:else if n.bdg}<span class="bdg">{n.bdg}</span>{/if}
      </button>
    {/each}
    <div class="side-foot"></div>
  </aside>

  <div class="main">
    <header class="topbar">
      <div class="leds">
        <div class="led"><span class="dot g"></span><span>Internet</span></div>
        <div class="led"><span class="dot {fyOn ? 'g' : 'r'}"></span><span>FotoYu</span></div>
        <div class="led"><span class="dot {ftpOn ? 'a' : 'r'}"></span><span>Kamera Wi-Fi</span></div>
      </div>
      <button class="tbtn" style="margin-left:auto" title="Terang/Gelap" on:click={toggleTheme} aria-label="Ganti tema">
        <svg viewBox="0 0 24 24"><path d="M21 12.8A9 9 0 1111.2 3a7 7 0 009.8 9.8z"/></svg>
      </button>
      <div class="me"><div class="ava">{avatar}</div>{acct && acct.username ? "@" + acct.username : "belum masuk"}</div>
    </header>

    <div class="work">
      {#if screen === "unggah"}<Unggah on:goto={(e) => { screen = e.detail.screen; settingsTab = e.detail.tab; }} />
      {:else if screen === "pengaturan"}<Pengaturan bind:tab={settingsTab} />
      {:else if screen === "ranking"}<Ranking />
      {:else if screen === "hires"}<Hires />
      {:else if screen === "videofoto"}<VideoFoto />
      {:else if screen === "log"}<Log />
      {/if}
    </div>
  </div>
</div>
