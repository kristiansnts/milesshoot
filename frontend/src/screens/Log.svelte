<script>
  import { onMount } from "svelte";
  import { logs as liveLogs, config } from "../lib/stores";
  import { api, isDesktop } from "../lib/api";

  let lines = [];
  let level = "all";
  let q = "";
  let file = "";
  let copied = false, reportMsg = "";

  $: cfg = $config;
  $: debugOn = cfg && cfg.debug;

  async function load() {
    try {
      const r = await api.logTail(800);
      if (r) lines = r;
    } catch {}
  }
  // muat ulang saat ada catatan baru (event log) + tiap 2 dtk sebagai jaring pengaman
  $: $liveLogs, load();
  onMount(() => {
    load();
    api.logFile().then((f) => (file = f || "")).catch(() => {});
    const t = setInterval(load, 2000);
    return () => clearInterval(t);
  });

  async function toggleDebug() {
    const c = await api.updateConfig(JSON.stringify({ debug: !debugOn }));
    if (c) config.set(c);
  }

  $: shown = lines.filter(
    (l) => (level === "all" || l.level === level) && (!q || (l.msg || "").toLowerCase().includes(q.toLowerCase()))
  );

  async function copyAll() {
    const text = shown.map((l) => fmt(l.t) + "  " + (l.level || "").toUpperCase() + "  " + l.msg).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {}
  }

  async function copyReport() {
    reportMsg = "";
    try {
      await navigator.clipboard.writeText(await api.diagReport());
      reportMsg = "Laporan tersalin — tempel ke chat bantuan.";
    } catch { reportMsg = "Gagal menyalin. Pakai \"Simpan laporan\"."; }
    setTimeout(() => (reportMsg = ""), 4000);
  }
  async function saveReport() {
    reportMsg = "";
    try { const p = await api.diagSave(); reportMsg = p ? "Tersimpan: " + p : ""; }
    catch (e) { reportMsg = "Gagal menyimpan: " + e; }
  }

  const fmt = (t) => new Date(t).toLocaleTimeString("id-ID", { hour12: false });
  const levels = [
    ["all", "Semua"],
    ["ok", "Berhasil"],
    ["info", "Info"],
    ["warn", "Peringatan"],
    ["error", "Error"],
  ];
</script>

<div class="head-row">
  <div>
    <h1 class="ttl">Log</h1>
    <div class="sub">Catatan rinci untuk menelusuri masalah — mis. saat menguji sambungan kamera.</div>
  </div>
  <div style="display:flex;gap:10px">
    <button class="btn s sm" on:click={copyAll}>{copied ? "Tersalin ✓" : "Salin"}</button>
    {#if isDesktop()}<button class="btn s sm" on:click={() => api.openLogFolder()}>Buka folder</button>{/if}
  </div>
</div>

<div class="single">
  <div class="panel">
    <div class="row">
      <div class="oi">
        <b>Mode debug</b>
        <span>catat tiap perintah &amp; balasan FTP dari kamera — nyalakan sebelum menguji, matikan setelah selesai</span>
      </div>
      <button class="sw" class:on={debugOn} on:click={toggleDebug} aria-label="Mode debug"><i></i></button>
    </div>
    <div class="row">
      <div class="oi">
        <b>Laporan masalah</b>
        <span>{reportMsg || "Ada kendala? Salin laporan lalu kirim — isinya versi, setelan, status antrean, foto bermasalah, dan log terbaru (tanpa password)."}</span>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn p sm" on:click={copyReport}>Salin laporan</button>
        {#if isDesktop()}<button class="btn s sm" on:click={saveReport}>Simpan file</button>{/if}
      </div>
    </div>
    {#if file}
      <div class="row">
        <div class="oi"><b>Berkas log hari ini</b><span>{file}</span></div>
      </div>
    {/if}
  </div>

  <div class="panel">
    <div class="ph">
      <h3>Catatan</h3>
      <span class="pc">{shown.length} baris{lines.length !== shown.length ? " (dari " + lines.length + ")" : ""}</span>
    </div>
    <div style="padding:0 18px 12px;display:flex;gap:10px;flex-wrap:wrap;align-items:center">
      <div class="seg">
        {#each levels as [v, label]}
          <button class:on={level === v} on:click={() => (level = v)}>{label}</button>
        {/each}
      </div>
      <input class="input" style="flex:1;min-width:180px" placeholder="Cari teks…" bind:value={q} />
    </div>

    <div class="logwrap" style="max-height:min(60vh,620px);padding:0 18px 16px">
      {#each shown as l}
        <div class="logl">
          <time>{fmt(l.t)}</time>
          <span class="lv {l.level}"></span>
          <p style="word-break:break-word">{l.msg}</p>
        </div>
      {/each}
      {#if shown.length === 0}
        <p class="muted" style="padding:16px 0">
          {lines.length ? "Tidak ada yang cocok dengan saringan." : "Belum ada catatan."}
        </p>
      {/if}
    </div>
  </div>

  <div class="panel" style="padding:14px 18px">
    <div class="muted" style="font-size:12.5px;line-height:1.6">
      <b>Cara menguji kamera:</b> nyalakan <b>Mode debug</b> → di kamera isi alamat/port/user/password dari
      Pengaturan → Sumber → Kamera Wi-Fi → coba kirim satu foto. Setiap langkah akan tercatat di sini
      (<code>-&gt;</code> perintah dari kamera, <code>&lt;-</code> balasan dari milesshoot). Password tidak ikut dicatat.
    </div>
  </div>
</div>
