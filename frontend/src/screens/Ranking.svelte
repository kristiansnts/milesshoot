<script>
  import { onMount, onDestroy } from "svelte";
  import { status, config, rankingTree, rankingRange } from "../lib/stores";
  import { api, onEvent } from "../lib/api";

  $: st = $status;
  $: fyOn = st && st.account && st.account.loggedIn;
  $: me = st && st.account;

  // FotoTree yang dilihat di Ranking — independen dari FotoTree tujuan unggah.
  // Disimpan di store ($rankingTree) supaya tetap ada saat pindah tab lalu kembali.
  $: tree = $rankingTree;

  const ranges = [
    ["today", "Hari ini"],
    ["d7", "7 hari"],
    ["d30", "30 hari"],
    ["all", "Semua"],
  ];
  let view = null; // RankView dari backend

  // --- pencarian FotoTree (punya sendiri, terpisah dari Pengaturan) ---
  let q = "", trees = [], searching = false;
  let showSearch = false;
  async function search() {
    searching = true;
    try { trees = (await api.searchTrees(q)) || []; } finally { searching = false; }
  }
  function pickTree(t) {
    view = null;
    rankingTree.set({ id: t.id, tag_id: t.tag_id, name: t.name, location: t.location, is_bib: t.is_bib });
    showSearch = false;
    trees = []; q = "";
    start(); // tree baru dipilih sendiri oleh user → wajar mulai memindai
  }

  // load(): tampilkan apa yang SUDAH ada (job yang masih jalan di backend, atau
  // cache dari pindaian terakhir) — TIDAK memulai pindaian baru. Dipakai saat
  // komponen baru dipasang (termasuk setelah pindah tab lalu kembali ke Ranking),
  // supaya tidak diam-diam mengulang dari nol padahal hasilnya sudah ada.
  // Terima `forTree` eksplisit (bukan hanya baca `tree` reaktif) supaya aman
  // dipanggil tepat setelah rankingTree.set() tanpa bergantung pada timing reactive.
  function load(forTree) {
    const t = forTree || tree;
    if (!t) return;
    api.rankingStatus(t.id, $rankingRange).then((v) => { if (v) view = v; });
  }

  // start(): memulai pindaian baru. Hanya dipanggil dari aksi EKSPLISIT user
  // (pilih FotoTree, ganti rentang, tekan Segarkan) — bukan dari remount biasa.
  function start() {
    if (!tree) return;
    view = null;
    api.rankingStart(tree.id, tree.name, $rankingRange).then((v) => { if (v) view = v; });
  }

  function setRange(r) {
    if (r === $rankingRange) return;
    rankingRange.set(r);
    start(); // rentang diganti sendiri oleh user → wajar mulai memindai ulang
  }

  function refresh() {
    start(); // paksa pindai ulang walau sudah ada cache (data FotoYu bisa berubah)
  }

  function cancel() {
    if (tree) api.rankingCancel(tree.id, $rankingRange);
  }

  let off;
  onMount(() => {
    // Default pertama kali: ikut FotoTree tujuan unggah, HANYA bila Ranking belum
    // pernah memilih sendiri (supaya pilihan manual di Ranking tidak ketiban tiap kembali).
    let initial = $rankingTree;
    if (!initial && $config && $config.event && $config.event.tree) {
      initial = $config.event.tree;
      rankingTree.set(initial);
    }
    load(initial); // tampilkan yang sudah ada — jangan mulai pindaian baru hanya karena remount
    off = onEvent("ranking", (v) => {
      if (v && tree && v.treeId === tree.id && v.range === $rankingRange) view = v;
    });
  });
  onDestroy(() => off && off());

  $: myRow = view && me && me.username
    ? view.rows.findIndex((r) => (r.username || "").toLowerCase() === me.username.toLowerCase())
    : -1;

  const fmtWhen = (t) => (t ? new Date(t).toLocaleString("id-ID", { hour12: false }) : "—");
  const medal = (i) => (i === 0 ? "🥇" : i === 1 ? "🥈" : i === 2 ? "🥉" : null);
</script>

<div class="head-row">
  <div>
    <h1 class="ttl">Ranking daun</h1>
    <div class="sub">{tree ? tree.name : "Pilih FotoTree untuk melihat rankingnya"}</div>
  </div>
  {#if tree && fyOn}
    <div class="seg">
      {#each ranges as [v, label]}<button class:on={$rankingRange === v} on:click={() => setRange(v)}>{label}</button>{/each}
    </div>
  {/if}
</div>

<div class="single">
  {#if !fyOn}
    <div class="panel" style="padding:34px 18px;text-align:center">
      <span class="muted">Masuk akun FotoYu dulu (Pengaturan → Akun) untuk melihat ranking.</span>
    </div>
  {:else}
    <!-- pemilih FotoTree — independen dari FotoTree tujuan unggah -->
    <div class="panel">
      <div class="ph">
        <h3>FotoTree</h3>
        {#if tree}<span class="pill up">{tree.name}</span>{/if}
        <button class="btn s sm" style="margin-left:auto" on:click={() => (showSearch = !showSearch)}>
          {tree ? "Ganti" : "Pilih FotoTree"}
        </button>
      </div>
      {#if showSearch}
        <div style="padding:0 18px 16px">
          <div style="display:flex;gap:8px;margin-bottom:10px">
            <input class="input" placeholder="Cari nama event / FotoTree" bind:value={q} on:keydown={(e) => e.key === "Enter" && search()} />
            <button class="btn s" on:click={search} disabled={searching}>{searching ? "…" : "Cari"}</button>
          </div>
          <div class="treelist">
            {#each trees as t}
              <button type="button" class="treerow" class:sel={tree && t.tag_id === tree.tag_id} on:click={() => pickTree(t)}>
                <div style="flex:1;min-width:0;text-align:left">
                  <b style="font-size:13.5px">{t.name}</b>
                  <div class="muted" style="font-size:12px">{t.category || ""}{t.count != null ? " · " + t.count + " foto" : ""}{t.official ? " · resmi" : ""}</div>
                </div>
              </button>
            {/each}
            {#if !trees.length && !searching}<span class="muted" style="font-size:12.5px">Ketik nama event lalu tekan Cari</span>{/if}
          </div>
        </div>
      {/if}
    </div>

    {#if !tree}
      <div class="panel" style="padding:34px 18px;text-align:center">
        <span class="muted">Belum ada FotoTree dipilih — cari di atas untuk melihat rankingnya.</span>
      </div>
    {:else}
      <div class="stats">
        <div class="stat lav">
          <span class="sl">Total daun di FotoTree</span>
          <div class="sv num">{(view && view.total) ? view.total.toLocaleString("id-ID") : "—"}</div>
          <div class="sm">sepanjang masa</div>
        </div>
        <div class="stat but">
          <span class="sl">Dipindai ({ranges.find((r) => r[0] === $rankingRange)?.[1]})</span>
          <div class="sv num">{view ? view.scanned.toLocaleString("id-ID") : 0}</div>
          <div class="sm">{view && !view.done ? "sedang memindai…" : view && view.cached ? "dari pindaian terakhir" : "selesai"}</div>
        </div>
        <div class="stat grey">
          <span class="sl">Akun muncul</span>
          <div class="sv num">{view ? view.accounts.toLocaleString("id-ID") : 0}</div>
          <div class="sm">{myRow >= 0 ? "kamu peringkat #" + (myRow + 1) : "akunmu belum muncul"}</div>
        </div>
      </div>

      <div class="panel">
        <div class="ph">
          <h3>Peringkat</h3>
          <span class="pc">
            {#if view && !view.done}memindai… {view.scanned} daun
            {:else if view && view.updatedAt}diperbarui {fmtWhen(view.updatedAt)}{#if view.cached} · tersimpan{/if}
            {:else}belum pernah dipindai{/if}
          </span>
        </div>

        {#if view && !view.done}
          <div style="padding:0 18px 12px">
            <!-- rentang "Semua": totalnya diketahui dari TreeDetail → progres pasti.
                 rentang waktu (hari ini/7/30): berhenti sebelum mencapai total daun,
                 jadi persentase tak bisa dihitung jujur → bar bergerak (tak pasti). -->
            {#if view.range === "all" && view.total > 0}
              <div class="bar" style="height:8px"><i style="width:{Math.min(100, Math.round((view.scanned / view.total) * 100))}%"></i></div>
            {:else}
              <div class="bar indet" style="height:8px"><i></i></div>
            {/if}
          </div>
        {/if}

        <div style="padding:0 18px 14px">
          {#if view && !view.done}
            <button class="btn s sm" on:click={cancel}>Batalkan</button>
          {:else}
            <button class="btn s sm" on:click={refresh}>Pindai ulang</button>
          {/if}
        </div>
        {#if view && view.error && view.error !== "Dibatalkan"}
          <div style="padding:0 18px 14px;color:var(--bad);font-size:12.5px">{view.error}</div>
        {/if}

        <table class="tbl">
          <thead><tr><th>#</th><th>Akun</th><th class="tR">Daun</th><th class="tR">Video</th><th class="tR">Terakhir</th></tr></thead>
          <tbody>
            {#if view}
              {#each view.rows as r, i}
                <tr style={myRow === i ? "background:var(--accent-soft)" : ""}>
                  <td class="num" style="font-weight:700">{medal(i) || i + 1}</td>
                  <td>
                    <div class="fname">
                      @{r.username}
                      {#if myRow === i}<span class="pill up">kamu</span>{/if}
                      {#if r.official}<span class="pill ok">resmi</span>{/if}
                    </div>
                  </td>
                  <td class="tR num" style="font-weight:700">{r.count.toLocaleString("id-ID")}</td>
                  <td class="tR num muted">{r.videos || 0}</td>
                  <td class="tR muted" style="font-size:12px">{fmtWhen(r.last)}</td>
                </tr>
              {/each}
            {/if}
            {#if !view || view.rows.length === 0}
              <tr><td colspan="5" class="muted" style="padding:26px 18px;text-align:center">
                {view && view.done ? "Belum ada daun di rentang ini." : "Memuat…"}
              </td></tr>
            {/if}
          </tbody>
        </table>
      </div>

      <p class="muted" style="font-size:12px;padding:0 2px">
        Dihitung dari daun FotoTree ini saja — bukan seluruh akun FotoYu-mu. Rentang "Hari ini/7 hari/30 hari" berhenti
        memindai begitu sampai daun yang lebih lama, jadi cepat walau FotoTree-nya besar.
      </p>
    {/if}
  {/if}
</div>
