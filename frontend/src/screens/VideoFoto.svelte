<script>
  import { onMount, onDestroy } from "svelte";
  import { api, isDesktop } from "../lib/api";

  // ---- pilihan (diingat per perangkat) ----
  const KEY = "vf-opts";
  let count = 10, crop23 = true, unique = true, tegak = "cw";
  try {
    const o = JSON.parse(localStorage.getItem(KEY) || "{}");
    if (o.count) count = o.count;
    if (typeof o.crop23 === "boolean") crop23 = o.crop23;
    if (typeof o.unique === "boolean") unique = o.unique;
    if (["", "cw", "ccw"].includes(o.tegak)) tegak = o.tegak;
  } catch {}
  $: { try { localStorage.setItem(KEY, JSON.stringify({ count, crop23, unique, tegak })); } catch {} }

  // ---- pekerjaan ----
  let files = [];       // video yang dipilih, siap diproses
  let fromFolder = false;
  let st = null;
  let err = "";
  let timer = null;

  $: running = !!(st && st.running);
  $: items = (st && st.items) || [];
  $: pct = st && st.total ? Math.round(((st.index - 1) / st.total) * 100) : 0;
  $: noFfmpeg = st && st.available === false;

  async function refresh() {
    try { st = await api.vfState(); } catch { /* abaikan */ }
    if (st && st.running && !timer) timer = setInterval(refresh, 700);
    if (!(st && st.running) && timer) { clearInterval(timer); timer = null; }
  }
  onMount(refresh);
  onDestroy(() => timer && clearInterval(timer));

  const name = (p) => p.split(/[\\/]/).pop();

  async function pickFiles() {
    err = "";
    const r = await api.vfPickFiles();
    if (r && r.length) { files = r; fromFolder = false; }
  }
  async function pickFolder() {
    err = "";
    const r = await api.vfPickFolder();
    if (r === null) return;
    if (!r.length) { err = "Tidak ada video (.mp4 / .mov / .m4v) di folder itu."; return; }
    files = r; fromFolder = true;
  }
  async function start() {
    err = "";
    try {
      st = await api.vfStart(files, JSON.stringify({ count, crop23, unique, tegak, skipDone: fromFolder }));
      files = [];
      refresh();
    } catch (e) { err = String(e); }
  }
  const cancel = () => api.vfCancel();
</script>

<div class="top"><h1>Video → Foto</h1><div class="sub muted">Ubah video pendek jadi foto siap jual</div></div>

{#if !isDesktop()}
  <div class="panel"><div class="muted" style="padding:16px 18px">Fitur ini hanya tersedia di aplikasi laptop.</div></div>
{:else}
  {#if noFfmpeg}
    <div class="panel"><div style="padding:14px 18px;color:var(--bad)">ffmpeg tidak ditemukan di komputer ini. Pasang dulu (mis. <code>brew install ffmpeg</code>) lalu buka ulang aplikasi.</div></div>
  {/if}

  <div class="panel">
    <div class="ph"><h3>Video</h3></div>
    <div style="padding:4px 18px 16px">
      <div style="display:flex;gap:8px;flex-wrap:wrap">
        <button class="btn p" on:click={pickFiles} disabled={running}>🎞 Pilih video</button>
        <button class="btn s" on:click={pickFolder} disabled={running}>📂 Pilih folder</button>
      </div>
      <div class="muted" style="font-size:12.5px;margin-top:10px">
        Pilih video pendek (±15 detik). Momen yang paling tajam dan cahayanya baik disimpan sebagai foto resolusi penuh di folder
        <b>Hasil Capture Video</b> (di samping video aslinya) lalu <b>langsung masuk antrean unggah</b> ke FotoYu, memakai event dan harga yang sedang dipakai di menu Unggah. "Pilih folder" memproses semua video di dalamnya; yang sudah pernah diproses dilewati.
      </div>
      {#if err}<div style="color:var(--bad);font-size:12.5px;margin-top:8px">{err}</div>{/if}
      {#if files.length}
        <div class="row" style="padding:12px 0 0;border:0">
          <div class="oi"><b>{files.length} video dipilih</b><span>{files.length === 1 ? name(files[0]) : name(files[0]) + " dan " + (files.length - 1) + " lainnya"}</span></div>
          <button class="btn p" on:click={start} disabled={running || noFfmpeg}>Mulai</button>
        </div>
      {/if}
    </div>
  </div>

  <div class="panel">
    <div class="ph"><h3>Pengaturan</h3></div>
    <div class="row">
      <div class="oi"><b>Foto dari tiap video</b><span>jumlah momen yang diambil</span></div>
      <div class="seg">
        {#each [5, 10, 15, 20] as n}<button class:on={count === n} on:click={() => (count = n)}>{n}</button>{/each}
      </div>
    </div>
    <div class="row">
      <div class="oi"><b>Atur tegak</b><span>Video yang tampil mendatar diputar jadi tegak. Yang sudah tegak (termasuk dari HP) tidak diputar lagi.</span></div>
      <div class="seg">
        <button class:on={tegak === "cw"} on:click={() => (tegak = "cw")}>Putar kanan</button>
        <button class:on={tegak === "ccw"} on:click={() => (tegak = "ccw")}>Putar kiri</button>
        <button class:on={tegak === ""} on:click={() => (tegak = "")}>Mati</button>
      </div>
    </div>
    <div class="row">
      <div class="oi"><b>Potong 2:3 tegak</b><span>Langsung siap dijual. Video tegak 9:16: bagian atas dibuang lebih banyak. Video lebar dipotong di tengah.</span></div>
      <button class="sw" class:on={crop23} on:click={() => (crop23 = !crop23)} aria-label="Potong 2:3 tegak"><i></i></button>
    </div>
    <div class="row">
      <div class="oi"><b>Lewati momen yang terlalu mirip</b><span>Supaya tidak dianggap foto dobel. Fotonya bisa lebih sedikit dari jumlah yang dipilih.</span></div>
      <button class="sw" class:on={unique} on:click={() => (unique = !unique)} aria-label="Lewati momen mirip"><i></i></button>
    </div>
    <div class="muted" style="font-size:12px;padding:8px 18px 14px">Tips: rekam 4K dengan shutter cepat (±1/100 atau lebih) supaya foto tidak buram. Foto di bawah 1 MB otomatis diperbesar (±2 MB) supaya lolos batas hi-res FotoYu; detail aslinya tidak bertambah. Foto yang sama persis dengan yang pernah diunggah tidak dimasukkan lagi.</div>
  </div>

  {#if running || items.length}
    <div class="panel">
      <div class="ph">
        <h3>{running ? "Memproses…" : "Hasil"}</h3>
        <span class="pc">{st.photos} foto · {st.queued} masuk antrean{st.total ? " · " + items.length + "/" + st.total + " video" : ""}</span>
      </div>
      {#if running}
        <div style="padding:4px 18px 14px">
          <div class="oi"><b>{st.current || "…"}</b><span>{st.stage} · video {st.index} dari {st.total}</span></div>
          <div style="height:6px;border-radius:3px;background:var(--inset);margin:10px 0"><div style="height:100%;width:{pct}%;border-radius:3px;background:var(--accent);transition:width .3s"></div></div>
          <button class="btn s sm" on:click={cancel} disabled={st.cancelling}>{st.cancelling ? "Membatalkan…" : "Batal"}</button>
        </div>
      {/if}
      {#if !running && st.queued > 0 && st.paused}
        <div style="padding:0 18px 12px;color:var(--warn);font-size:12.5px">Unggah sedang dijeda — {st.queued} foto menunggu di antrean. Buka menu Unggah dan tekan <b>Unggah</b> untuk mengirimnya.</div>
      {/if}
      {#each items as it}
        <div class="row">
          <div class="oi">
            <b>{name(it.video)}</b>
            {#if it.error}<span style="color:var(--bad)">{it.error}</span>
            {:else if it.skipped}<span>sudah pernah diproses — dilewati</span>
            {:else}<span>{it.files.length} foto{it.enlarged ? " · " + it.enlarged + " diperbesar" : ""}{it.similar ? " · " + it.similar + " momen dilewati (terlalu mirip)" : ""}</span>{/if}
          </div>
          {#if !it.error && !it.skipped}<button class="btn s sm" on:click={() => api.vfReveal(it.dir)}>Buka folder</button>{/if}
        </div>
      {/each}
    </div>
  {/if}
{/if}
