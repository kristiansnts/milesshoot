<script>
  import { onMount } from "svelte";
  import { hires, status, config } from "../lib/stores";
  import { api, isDesktop } from "../lib/api";

  $: h = $hires;
  $: items = (h && h.items) || [];
  $: st = $status;
  $: fyOn = st && st.account && st.account.loggedIn;
  $: folders = (($config && $config.hires) || {}).folders || [];

  let busy = false;
  async function refresh() {
    busy = true;
    try { const r = await api.hiresRefresh(); if (r) hires.set(r); } finally { busy = false; }
  }
  async function sendOne(id) {
    const r = await api.hiresSend(id);
    if (r) hires.set(r);
  }
  async function sendAll() {
    busy = true;
    try { const r = await api.hiresSendAll(); if (r) hires.set(r); } finally { busy = false; }
  }
  const addFolder = () => api.hiresAddFolder();
  const delFolder = (p) => api.hiresRemoveFolder(p);

  $: autoOn = (($config && $config.hires) || {}).auto || false;
  async function toggleAuto() {
    const c = await api.hiresSetAuto(!autoOn);
    if (c) config.set(c);
  }

  // ---- LUT khusus hi-res ----
  /** @type {{name: string, path: string}[]} */
  let luts = [];
  const loadLuts = async () => { try { luts = (await api.listLuts()) || []; } catch { luts = []; } };
  $: hiresCfg = ($config && $config.hires) || {};
  $: lutPath = hiresCfg.lut || "";
  $: lutStrength = hiresCfg.lutStrength == null ? 100 : hiresCfg.lutStrength;
  // LUT yang dipilih tapi sudah tak ada di pustaka (mis. file dihapus manual) → beri tahu, jangan diam.
  $: lutMissing = lutPath && luts.length > 0 && !luts.some((l) => l.path === lutPath);
  /** @param {string} path */
  async function pickLut(path) {
    const c = await api.setLut("hires", path);
    if (c) config.set(c);
  }
  /** @param {number} v */
  async function setStrength(v) {
    const c = await api.updateConfig(JSON.stringify({ hires: { lutStrength: v } }));
    if (c) config.set(c);
  }

  onMount(() => { loadLuts(); if (fyOn) refresh(); });

  const short = (p) => (p && p.length > 52 ? "…" + p.slice(-51) : p || "—");
  const badge = { md5: ["ok", "Cocok pasti"], name: ["wait", "Cocok nama"], none: ["dup", "Tidak ketemu"] };
</script>

<div class="head-row">
  <div>
    <h1 class="ttl">Hi-res</h1>
    <div class="sub">Foto/video terjual menunggu file asli. Selama belum dikirim, pesanannya menggantung.</div>
  </div>
  <div style="display:flex;gap:10px">
    <button class="btn s sm" on:click={refresh} disabled={busy || !fyOn}>{busy ? "…" : "Segarkan"}</button>
    <button class="btn p sm" on:click={sendAll} disabled={busy || !h || !h.exact}>Kirim semua cocok pasti{h && h.exact ? " (" + h.exact + ")" : ""}</button>
  </div>
</div>

<div class="single">
  {#if !fyOn}
    <div class="panel" style="padding:34px 18px;text-align:center" class:muted={true}>
      Masuk akun FotoYu dulu (Pengaturan → Akun) untuk melihat pesanan hi-res.
    </div>
  {:else}
    <div class="stats">
      <div class="stat mint"><span class="sl">Cocok pasti</span><div class="sv num">{(h && h.exact) || 0}</div><div class="sm">sidik file sama — aman dikirim</div></div>
      <div class="stat but"><span class="sl">Cocok nama</span><div class="sv num">{(h && h.byName) || 0}</div><div class="sm">isi beda — periksa dulu</div></div>
      <div class="stat grey"><span class="sl">Tidak ketemu</span><div class="sv num">{(h && h.none) || 0}</div><div class="sm">tambahkan folder file asli</div></div>
    </div>

    {#if h && h.error}
      <div class="panel" style="padding:14px 18px;color:var(--bad)">{h.error}</div>
    {/if}

    <div class="panel">
      <div class="row">
        <div class="oi">
          <b>Kirim otomatis</b>
          <span>tiap 5 menit, pesanan "cocok pasti" dikirim sendiri — "cocok nama" selalu menunggu dicek manual</span>
        </div>
        <button class="sw" class:on={autoOn} on:click={toggleAuto} aria-label="Kirim otomatis"><i></i></button>
      </div>
    </div>

    <div class="panel">
      <div class="row">
        <div class="oi">
          <b>LUT hi-res</b>
          <span>warna dipakai pada file asli saat dikirim (file di laptop tidak diubah). Foto di bawah 1 MB otomatis diperbesar jadi ±2 MB</span>
        </div>
        <select class="input" style="max-width:220px" value={lutPath} on:change={(e) => pickLut(e.currentTarget.value)} disabled={!isDesktop()}>
          <option value="">Tanpa LUT</option>
          {#each luts as l}<option value={l.path}>{l.name.replace(/\.cube$/i, "")}</option>{/each}
        </select>
      </div>
      {#if lutPath}
        <div class="row">
          <div class="oi"><b>Kekuatan</b><span>{lutStrength}%</span></div>
          <input type="range" min="0" max="100" value={lutStrength} on:change={(e) => setStrength(+e.currentTarget.value)} />
        </div>
      {/if}
      {#if lutMissing}
        <div style="padding:0 18px 14px;color:var(--bad);font-size:12.5px">File LUT yang dipilih tidak ada lagi di pustaka — pengiriman hi-res akan gagal sampai LUT diganti atau dikosongkan.</div>
      {/if}
    </div>

    <div class="panel">
      <div class="ph">
        <h3>Pesanan</h3>
        <span class="pc">{items.length} item{h && h.sent ? " · " + h.sent + " terkirim" : ""}</span>
      </div>
      <table class="tbl">
        <thead><tr><th>File</th><th>Kecocokan</th><th>Lokasi file asli</th><th class="tR">Aksi</th></tr></thead>
        <tbody>
          {#each items as it}
            <tr>
              <td><div class="fname">
                <span class="fico">{it.kind === "video" ? "🎬" : "📷"}</span>{it.title}
              </div></td>
              <td><span class="pill {badge[it.match.type][0]}">{badge[it.match.type][1]}</span></td>
              <td class="muted" style="font-size:12px">{short(it.match.path)}</td>
              <td class="tR">
                {#if it.sent}
                  <span class="pill ok">Terkirim</span>
                {:else if it.error}
                  <span class="pill err" title={it.error}>Gagal</span>
                  <button class="btn s sm" style="margin-left:6px" on:click={() => sendOne(it.id)}>Ulangi</button>
                {:else if it.match.type === "none"}
                  <span class="muted">—</span>
                {:else}
                  <button class="btn {it.match.type === 'md5' ? 'p' : 's'} sm" on:click={() => sendOne(it.id)}>Kirim</button>
                {/if}
              </td>
            </tr>
          {/each}
          {#if items.length === 0}
            <tr><td colspan="4" class="muted" style="padding:26px 18px;text-align:center">
              {h && h.loadedAt ? "Tidak ada pesanan hi-res." : "Tekan Segarkan untuk mengambil daftar dari FotoYu."}
            </td></tr>
          {/if}
        </tbody>
      </table>
    </div>

    <div class="panel">
      <div class="ph"><h3>Folder file asli</h3><span class="pc">selain folder yang dipantau</span></div>
      <div style="padding:4px 18px 16px">
        <div class="chips" style="margin-bottom:10px">
          {#each folders as f}
            <span class="chip2">{short(f)}<button on:click={() => delFolder(f)} title="Hapus">×</button></span>
          {/each}
          {#if !folders.length}<span class="muted" style="font-size:12.5px">Belum ada folder tambahan</span>{/if}
        </div>
        {#if isDesktop()}<button class="btn s sm" on:click={addFolder}>Tambah folder</button>{/if}
        <div class="muted" style="font-size:12px;margin-top:8px">
          Folder "Sudah upload" otomatis ikut dicari — jadi foto yang sudah dipindah tetap ketemu.
        </div>
      </div>
    </div>
  {/if}
</div>
