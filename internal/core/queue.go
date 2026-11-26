package core

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Pesan yang menandakan gangguan jaringan sementara (bukan penolakan FotoYu) → diulang, bukan gagal.
var reNet = regexp.MustCompile(`(?i)ECONN|ETIMEDOUT|EAI_AGAIN|ENOTFOUND|socket|fetch failed|terhubung|sibuk|menjawab|timeout|connection|refused|reset`)

// Antrean upload: anti-duplikat MD5, olah & unggah paralel sesuai mode performa, auto-retry backoff.
// Port dari queue.js. Video diproses lewat VideoPrepareFn (di-set engine; default menolak sopan).

type limits struct{ uploads, process, frames int }

func perfLimits(mode string) limits {
	switch mode {
	case "hemat":
		return limits{2, 1, 1}
	case "ngebut":
		n := runtime.NumCPU() - 1
		if n < 1 {
			n = 1
		}
		return limits{8, n, 2}
	case "seimbang":
		return limits{4, 2, 2}
	default: // auto
		if runtime.NumCPU() <= 2 {
			return limits{4, 1, 1} // seimbang ringan untuk laptop lama
		}
		return limits{4, 2, 2}
	}
}

// gate: pembatas pekerjaan berat bersamaan (port dari Gate di queue.js).
type gate struct {
	mu      sync.Mutex
	busy    int
	waiters []chan struct{}
}

func (g *gate) enter(limit int) {
	g.mu.Lock()
	if g.busy < limit {
		g.busy++
		g.mu.Unlock()
		return
	}
	ch := make(chan struct{})
	g.waiters = append(g.waiters, ch)
	g.mu.Unlock()
	<-ch
	g.mu.Lock()
	g.busy++
	g.mu.Unlock()
}

func (g *gate) leave() {
	g.mu.Lock()
	g.busy--
	var next chan struct{}
	if len(g.waiters) > 0 {
		next = g.waiters[0]
		g.waiters = g.waiters[1:]
	}
	g.mu.Unlock()
	if next != nil {
		close(next)
	}
}

// VideoPrepareFn disuntik engine (ffmpeg). lut = LUT video (kalau diaktifkan, tiap frame
// pratinjau diwarnai sebelum di-zip — gaya MAS EDY: opts.grade di prepare()). Default: tolak
// dengan pesan jelas.
var VideoPrepareFn = func(path string, orientation string, frames int, lut LUT) (PreparedVideo, error) {
	return PreparedVideo{}, &UploadError{Msg: "Upload video belum didukung di versi ini", Permanent: true}
}

type UploadQueue struct {
	cfg       *Config
	store     *Store
	proc      *Processor
	uploaders map[string]Uploader
	emit      Emitter
	username  func() string

	mu          sync.Mutex
	active      int
	paused      bool
	pauseReason string
	pauseMsg    string

	busyUntil  int64
	busyStreak int
	busyKind   string
	netErrs    []int64
	busyLogged int64
	rejectKey  string
	rejectAt   int64
	rejectList []string

	setsMu         sync.Mutex
	duplicatePaths map[string]bool
	skippedVideos  map[string]bool
	skippedParity  map[string]bool
	hashing        map[string]bool

	// parityHadWork: dipakai auto-jeda saringan nomor eksklusif (lihat pump()). Hanya
	// disentuh dari loop() / pump(), jadi aman tanpa mu sendiri (satu goroutine).
	parityHadWork bool

	statsMu     sync.Mutex
	doneTimes   []int64
	bytesOut    int64
	dupCount    int
	sentWindow  []sentSample // bytes terunggah + waktunya, 120 dtk terakhir — buat hitung Mbps
	activeSince int64        // 0 = antrean kosong; selain itu waktu mulai "sesi unggah" yang sedang berjalan

	processGate gate
	uploadGate  gate
	hashGate    gate

	trigger chan struct{}
	stop    chan struct{}
}

func NewQueue(cfg *Config, store *Store, proc *Processor, uploaders map[string]Uploader, emit Emitter, username func() string) *UploadQueue {
	q := &UploadQueue{
		cfg: cfg, store: store, proc: proc, uploaders: uploaders, emit: emit, username: username,
		duplicatePaths: map[string]bool{}, skippedVideos: map[string]bool{}, skippedParity: map[string]bool{},
		hashing: map[string]bool{},
		trigger: make(chan struct{}, 1), stop: make(chan struct{}),
	}
	q.paused = cfg.Get().AutoUpload == false
	if q.paused {
		q.pauseReason = "manual"
	}
	return q
}

func (q *UploadQueue) Start() {
	go q.loop()
}

func (q *UploadQueue) Stop() { close(q.stop) }

func (q *UploadQueue) loop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-q.stop:
			return
		case <-t.C:
			q.pump()
		case <-q.trigger:
			q.pump()
		}
	}
}

func (q *UploadQueue) wake() {
	select {
	case q.trigger <- struct{}{}:
	default:
	}
}

func nowMs() int64 { return time.Now().UnixMilli() }

// ---------- set path yang "dikenal" ----------

func (q *UploadQueue) isKnownPath(p string) bool {
	if q.store.KnownPath(p) {
		return true
	}
	q.setsMu.Lock()
	defer q.setsMu.Unlock()
	return q.duplicatePaths[p] || q.skippedVideos[p] || q.skippedParity[p]
}

func (q *UploadQueue) setPath(m map[string]bool, p string, on bool) {
	q.setsMu.Lock()
	if on {
		m[p] = true
	} else {
		delete(m, p)
	}
	q.setsMu.Unlock()
}

// VideosEnabledChanged: lupakan video yang tadinya dilewati supaya dipindai ulang.
func (q *UploadQueue) VideosEnabledChanged() {
	q.setsMu.Lock()
	q.skippedVideos = map[string]bool{}
	q.setsMu.Unlock()
}

// FiltersChanged: saringan ganjil/genap diubah → file yang tadinya dilewati dipindai ulang.
func (q *UploadQueue) FiltersChanged() {
	q.setsMu.Lock()
	q.skippedParity = map[string]bool{}
	q.setsMu.Unlock()
}

// ---------- menerima file baru dari scanner ----------

func (q *UploadQueue) AddFile(path string, size int64) {
	if q.isKnownPath(path) {
		return
	}
	cfg := q.cfg.Get()
	kind := "photo"
	if isVideo(path) {
		kind = "video"
	}
	if kind == "video" && !cfg.Video.Enabled {
		q.setPath(q.skippedVideos, path, true)
		return
	}
	if cfg.Parity.Only && cfg.Parity.Mode != "all" {
		if pf := ParityOf(path); pf != "" && pf != cfg.Parity.Mode {
			q.setPath(q.skippedParity, path, true)
			return
		}
	}
	q.setPath(q.duplicatePaths, path, true) // kunci sementara selama hashing
	q.setPath(q.hashing, path, true)

	q.hashGate.enter(2)
	sum, herr := md5File(path)
	q.hashGate.leave()
	q.setPath(q.hashing, path, false)

	if herr != nil {
		q.setPath(q.duplicatePaths, path, false)
		return
	}

	if q.store.Has(sum) {
		prev := q.store.Get(sum)
		q.statsMu.Lock()
		q.dupCount++
		q.statsMu.Unlock()
		moved := ""
		if prev != nil && (prev.Status == "done" || prev.Dup == "own") {
			moved = q.moveToArchive(path, cfg)
		}
		if moved != "" {
			q.setPath(q.duplicatePaths, moved, true)
			q.setPath(q.duplicatePaths, path, false)
		}
		acct := ""
		if prev != nil && prev.Account != "" {
			acct = " ke @" + prev.Account
		}
		q.emit.Log("info", fmt.Sprintf("Dilewati (file sama sudah pernah diunggah%s): %s", acct, filepath.Base(path)))
		q.emit.Emit("change", nil)
		return // tetap di duplicatePaths agar tak di-hash ulang
	}

	q.setPath(q.duplicatePaths, path, false)
	q.store.Add(&Record{
		MD5: sum, Path: path, Name: filepath.Base(path), Size: size, Kind: kind,
		MTime: fileMTimeMs(path), Status: "pending", AddedAt: nowMs(),
		Event: cfg.Event.Name, Destination: cfg.Destination,
	})
	q.emit.Log("info", "Masuk antrean: "+filepath.Base(path))
	q.emit.Emit("change", nil)
	q.wake()
}

// AddDirect memasukkan satu foto ke antrean atas permintaan pemakai (mis. hasil Video → Foto),
// di mana pun letak filenya. Tidak kena saringan ganjil/genap, tidak harus berada di folder
// pantau, dan setelah terunggah tidak dipindah (Requeue). false = sudah pernah diunggah/ada di antrean.
func (q *UploadQueue) AddDirect(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	q.hashGate.enter(2)
	sum, herr := md5File(path)
	q.hashGate.leave()
	if herr != nil {
		return false, herr
	}
	if q.store.Has(sum) {
		return false, nil
	}
	cfg := q.cfg.Get()
	q.store.Add(&Record{
		MD5: sum, Path: path, Name: filepath.Base(path), Size: fi.Size(), Kind: "photo",
		MTime: fileMTimeMs(path), Status: "pending", AddedAt: nowMs(), Requeue: true,
		Event: cfg.Event.Name, Destination: cfg.Destination,
	})
	q.emit.Log("info", "Masuk antrean: "+filepath.Base(path))
	q.emit.Emit("change", nil)
	q.wake()
	return true, nil
}

// ---------- pilih & jalankan ----------

func (q *UploadQueue) nextJob(cfg Settings) *Record {
	now := nowMs()
	newest := cfg.UploadOrder != "oldest"
	var best *Record
	bestP, bestT := -1, int64(0)
	q.store.ForEach(func(r *Record) {
		if r.Status != "pending" || r.NextAttempt > now {
			return
		}
		if !q.inCurrent(r, cfg) {
			return
		}
		if parityBlocked(r, cfg) {
			return
		}
		if r.Kind == "video" && cfg.Video.Confirm && !r.OriOK {
			return
		}
		aged := now-r.AddedAt > 30*60*1000
		p := 0
		if aged {
			p++
		}
		t := r.MTime
		if t == 0 {
			t = r.AddedAt
		}
		if best == nil || p > bestP || (p == bestP && ((newest && t > bestT) || (!newest && t < bestT))) {
			cp := *r
			best = &cp
			bestP = p
			bestT = t
		}
	})
	return best
}

// parityBlocked: saringan nomor eksklusif aktif dan file ini bernomor lain. File itu dibiarkan
// menunggu (naik lagi bila saringan dikembalikan ke "Semua"); file tanpa nomor tidak disaring.
func parityBlocked(r *Record, cfg Settings) bool {
	if !cfg.Parity.Only || cfg.Parity.Mode == "all" || r.Requeue {
		return false
	}
	pf := ParityOf(firstNonEmpty(r.Name, r.Path))
	return pf != "" && pf != cfg.Parity.Mode
}

// hasParityPending: masih ada antrean menunggu yang lolos saringan nomor. File bernomor lain
// yang tertinggal di antrean tidak dihitung, supaya tidak menahan auto-jeda selamanya.
func (q *UploadQueue) hasParityPending(cfg Settings) bool {
	found := false
	q.store.ForEach(func(r *Record) {
		if !found && r.Status == "pending" && !parityBlocked(r, cfg) {
			found = true
		}
	})
	return found
}

// Hanya file dari folder sumber yang sedang dipilih (utama ATAU tambahan) yang diunggah
// (event lama tidak naik ke event baru).
func (q *UploadQueue) inCurrent(r *Record, cfg Settings) bool {
	if r.Requeue {
		return true
	}
	roots := AllWatchFolders(cfg)
	if len(roots) == 0 || r.Path == "" {
		return true
	}
	return RootFor(cfg, r.Path) != ""
}

func (q *UploadQueue) pump() {
	q.mu.Lock()
	now := nowMs()
	if q.paused || (q.busyUntil != 0 && now < q.busyUntil) {
		q.mu.Unlock()
		return
	}
	cfg := q.cfg.Get()
	lim := perfLimits(cfg.Performance)
	max := lim.uploads + lim.process
	if q.busyStreak > 0 {
		max = 1 // setelah server sibuk: coba 1 dulu
	}
	started := false
	for q.active < max {
		job := q.nextJob(cfg)
		if job == nil {
			break
		}
		q.store.Update(job.MD5, func(r *Record) { r.Status = "uploading"; r.Stage = "mengolah" })
		q.active++
		started = true
		go q.run(*job)
	}
	active := q.active
	q.mu.Unlock()
	if started {
		q.emit.Emit("change", nil)
	}

	// Saringan nomor eksklusif ("hanya ganjil"/"hanya genap"): begitu antreannya habis,
	// berhenti sendiri — supaya tidak diam-diam lanjut mengunggah file baru yang masuk
	// setelah batch ini (biasanya jadi tanggung jawab laptop/akun lain).
	if cfg.Parity.Only && cfg.Parity.Mode != "all" {
		if active > 0 || q.store.Counts().Uploading > 0 || q.hasParityPending(cfg) {
			q.parityHadWork = true
		} else if q.parityHadWork {
			q.parityHadWork = false
			q.Pause("manual")
			label := "ganjil"
			if cfg.Parity.Mode == "even" {
				label = "genap"
			}
			q.emit.Log("info", fmt.Sprintf("Semua file %s selesai diunggah — unggah otomatis dijeda.", label))
		}
	} else {
		q.parityHadWork = false
	}
}

func (q *UploadQueue) run(job Record) {
	defer func() {
		q.mu.Lock()
		q.active--
		q.mu.Unlock()
		q.wake()
	}()

	if !fileExists(job.Path) {
		q.store.Update(job.MD5, func(r *Record) { r.Status = "pending"; r.Stage = ""; r.NextAttempt = nowMs() + 30000 })
		return
	}
	cfg := q.cfg.Get()
	up := q.uploaders[cfg.Destination]
	if up == nil {
		up = q.uploaders["local"]
	}
	kind := job.Kind
	if kind == "" {
		if isVideo(job.Path) {
			kind = "video"
		} else {
			kind = "photo"
		}
	}
	lim := perfLimits(cfg.Performance)

	var res UploadResult
	var bytesIn, bytesOut int64
	var thumbSrc []byte
	var extraKind string = kind
	var vmeta struct{ w, h, dur int }
	var err error

	meta := Meta{
		Event: job.Event, OriginalName: job.Name, MD5: job.MD5, FilePath: job.Path,
	}
	if cfg.Event.Tree != nil {
		meta.Tree = cfg.Event.Tree
	}

	if kind == "video" {
		q.processGate.enter(lim.process)
		pv, perr := VideoPrepareFn(job.Path, orientationOf(cfg, job), lim.frames, lutFor(cfg, "video"))
		q.processGate.leave()
		if perr != nil {
			err = perr
		} else {
			meta.Price = pickPrice(cfg, true)
			q.uploadGate.enter(lim.uploads)
			q.store.Update(job.MD5, func(r *Record) { r.Stage = "mengunggah" })
			q.emit.Emit("change", nil)
			res, err = up.UploadVideo(VideoJob{Video: pv, Meta: meta, Cfg: cfg})
			q.uploadGate.leave()
			bytesIn, bytesOut, thumbSrc = job.Size, int64(len(pv.Zip)), pv.FirstFrame
			vmeta.w, vmeta.h, vmeta.dur = pv.Width, pv.Height, pv.Duration
		}
	} else {
		q.processGate.enter(lim.process)
		prep, perr := q.proc.Prepare(job.Path, cfg.Compress, lutFor(cfg, "photo"), cfg.Watermark)
		q.processGate.leave()
		if perr != nil {
			err = perr
		} else {
			meta.Price = pickPrice(cfg, false)
			meta.OriginalAt = prep.OriginalAt
			meta.LUT = prep.LUTApplied
			fileName := job.Name
			if prep.Compressed {
				fileName = replaceExt(job.Name, ".jpg")
			}
			q.uploadGate.enter(lim.uploads)
			q.store.Update(job.MD5, func(r *Record) { r.Stage = "mengunggah" })
			q.emit.Emit("change", nil)
			res, err = up.Upload(PhotoJob{Buffer: prep.Buffer, FileName: fileName, Width: prep.Width, Height: prep.Height, Meta: meta, Cfg: cfg})
			q.uploadGate.leave()
			bytesIn, bytesOut, thumbSrc = prep.BytesIn, prep.BytesOut, prep.Buffer
		}
	}

	if err == nil {
		hasThumb := q.proc.SaveThumb(job.MD5, thumbSrc)
		finalPath := job.Path
		if !job.Requeue {
			if mv := q.moveToArchive(job.Path, cfg); mv != "" {
				finalPath = mv
			}
		}
		treeName := ""
		if cfg.Event.Tree != nil {
			treeName = cfg.Event.Tree.Name
		}
		q.store.Update(job.MD5, func(r *Record) {
			r.Status = "done"
			r.Stage = ""
			r.DoneAt = nowMs()
			r.Error = ""
			r.RemoteID = res.RemoteID
			r.Path = finalPath
			r.Destination = cfg.Destination
			r.Account = res.Account
			r.Price = meta.Price
			r.Thumb = hasThumb
			r.Tree = treeName
			r.Kind = extraKind
			r.BytesOut = bytesOut
		})
		q.statsMu.Lock()
		now := nowMs()
		q.doneTimes = append(q.doneTimes, now)
		if len(q.doneTimes) > 500 {
			q.doneTimes = q.doneTimes[len(q.doneTimes)-500:]
		}
		q.bytesOut += bytesOut
		q.sentWindow = append(q.sentWindow, sentSample{t: now, bytes: bytesOut})
		cutoff := now - 120000
		i := 0
		for i < len(q.sentWindow) && q.sentWindow[i].t < cutoff {
			i++
		}
		if i > 0 {
			q.sentWindow = q.sentWindow[i:]
		}
		q.statsMu.Unlock()
		_ = bytesIn
		if q.busyStreak > 0 {
			q.busyStreak = 0
			q.busyUntil = 0
			q.emit.Log("ok", "Koneksi normal lagi — unggahan jalan")
		}
		suffix := ""
		if kind == "video" {
			suffix = " (video)"
		}
		q.emit.Log("ok", "Terunggah: "+job.Name+suffix+uploadedDetail(cfg, res, meta.Price, bytesOut))
		return
	}

	q.handleError(job, kind, cfg, err)
}

// ---------- penanganan error (port dari run() di queue.js) ----------

// failContext: rincian teknis yang ditempel di catatan gagal, supaya satu baris log sudah cukup
// untuk memahami masalahnya (kode HTTP, tahap, percobaan ke-berapa, ukuran, balasan FotoYu).
func failContext(job Record, ue *UploadError, attempts, max int) string {
	parts := []string{"tahap=" + firstNonEmpty(job.Stage, "-")}
	if ue != nil && ue.Status != 0 {
		parts = append(parts, fmt.Sprintf("http=%d", ue.Status))
	}
	parts = append(parts, fmt.Sprintf("percobaan=%d/%d", attempts, max+1),
		fmt.Sprintf("ukuran=%dKB", job.Size/1024), "jenis="+job.Kind, "tujuan="+job.Destination)
	if len(job.MD5) >= 8 {
		parts = append(parts, "id="+job.MD5[:8])
	}
	if ue != nil {
		var f []string
		if ue.Permanent {
			f = append(f, "permanen")
		}
		if ue.Account {
			f = append(f, "akun")
		}
		if ue.Local {
			f = append(f, "olah-gambar")
		}
		if ue.Put {
			f = append(f, "kirim-penyimpanan")
		}
		if ue.Duplicate != "" {
			f = append(f, "dobel="+ue.Duplicate)
		}
		if len(f) > 0 {
			parts = append(parts, "ciri="+strings.Join(f, ","))
		}
		if d := strings.TrimSpace(ue.Detail); d != "" {
			if len(d) > 400 {
				d = d[:400] + "…"
			}
			parts = append(parts, "fotoyu="+strconv.Quote(d))
		}
	}
	return " [" + strings.Join(parts, " · ") + "]"
}

func (q *UploadQueue) handleError(job Record, kind string, cfg Settings, err error) {
	ue, _ := err.(*UploadError)
	msg := err.Error()

	if ue != nil && ue.Pause {
		q.store.Update(job.MD5, func(r *Record) { r.Status = "pending"; r.Stage = ""; r.Error = msg; r.NextAttempt = 0 })
		q.mu.Lock()
		if !q.paused {
			q.paused = true
			if ue.Account {
				q.pauseReason = "account"
			} else {
				q.pauseReason = "setting"
			}
			q.pauseMsg = msg
			detail := ""
			if ue.Detail != "" {
				detail = " (FotoYu: " + ue.Detail + ")"
			}
			q.emit.Log("error", "Upload dijeda: "+msg+detail+failContext(job, ue, job.Attempts+1, cfg.MaxRetries))
		}
		q.mu.Unlock()
		return
	}

	if ue != nil && ue.Duplicate != "" {
		own := ue.Duplicate == "own"
		moved := ""
		if own {
			moved = q.moveToArchive(job.Path, cfg)
		}
		q.store.Update(job.MD5, func(r *Record) {
			r.Status = "duplicate"
			r.Dup = ue.Duplicate
			r.Stage = ""
			r.Error = msg
			r.DoneAt = nowMs()
			r.RemoteID = ue.ContentID
			r.Account = q.username()
			if moved != "" {
				r.Path = moved
			}
		})
		level := "warn"
		verb := "Ditolak FotoYu"
		if own {
			level, verb = "info", "Dilewati"
		}
		q.emit.Log(level, fmt.Sprintf("%s: %s — %s", verb, job.Name, msg))
		return
	}

	if ue != nil && ue.Local {
		tries := job.LocalTries + 1
		giveUp := tries > 20
		q.store.Update(job.MD5, func(r *Record) {
			r.LocalTries = tries
			r.Error = msg
			r.Stage = ""
			if giveUp {
				r.Status = "failed"
				r.NextAttempt = 0
			} else {
				r.Status = "pending"
				r.NextAttempt = nowMs() + 5000
			}
		})
		if giveUp {
			q.emit.Log("error", "Gagal: "+job.Name+" — "+msg+failContext(job, ue, tries, 19))
		}
		return
	}

	status := 0
	permanent := false
	if ue != nil {
		status = ue.Status
		permanent = ue.Permanent
	}
	busy := !permanent && (status == 0 || status == 408 || status == 429 || status >= 500 ||
		reNet.MatchString(msg))

	now := nowMs()
	if busy {
		q.mu.Lock()
		q.netErrs = filterRecent(q.netErrs, now, 30000)
		q.netErrs = append(q.netErrs, now)
		nErr := len(q.netErrs)
		streak := q.busyStreak
		q.mu.Unlock()

		serverBusy := status == 429 || status >= 500
		// Sinyal putus sekali (bukan jawaban sibuk FotoYu): cukup foto ini diulang.
		if !serverBusy && nErr < 2 && streak == 0 {
			q.store.Update(job.MD5, func(r *Record) {
				r.Status = "pending"
				r.Stage = ""
				r.Error = "Koneksi putus sesaat, diulang: " + msg
				r.NextAttempt = nowMs() + 3000
			})
			q.emit.Log("info", "Koneksi putus sesaat: "+job.Name+", diulang 3 dtk lagi")
			return
		}
		q.mu.Lock()
		q.busyStreak++
		streak = q.busyStreak
		q.busyKind = "net"
		if serverBusy {
			q.busyKind = "server"
		}
		q.mu.Unlock()
		capS := 15
		if serverBusy {
			capS = 60
		}
		wait := int(math.Min(float64(capS), 10*math.Pow(2, math.Min(3, float64(streak-1)))))*1000 + rand.Intn(pick(serverBusy, 5000, 2000))
		until := now + int64(wait)
		q.mu.Lock()
		if q.busyUntil == 0 || until > q.busyUntil {
			q.busyUntil = until
		}
		logNow := q.busyLogged == 0 || now-q.busyLogged > 60000
		if logNow {
			q.busyLogged = now
		}
		q.mu.Unlock()
		q.store.Update(job.MD5, func(r *Record) {
			r.Status = "pending"
			r.Stage = ""
			r.Error = "Menunggu server FotoYu: " + msg
			r.NextAttempt = until
		})
		if logNow {
			if serverBusy {
				q.emit.Log("warn", fmt.Sprintf("Server FotoYu sibuk — istirahat %d dtk lalu lanjut sendiri. Foto aman di antrean.", wait/1000))
			} else {
				q.emit.Log("warn", fmt.Sprintf("Internet terputus — foto aman di antrean, dicoba lagi tiap %d dtk.", wait/1000))
			}
		}
		time.AfterFunc(time.Duration(wait+200)*time.Millisecond, q.pump)
		return
	}

	// Penolakan permanen berturut sama = masalah sesi → kembalikan & jeda.
	if permanent && ue != nil && !ue.Missing && ue.Status != 0 { // Status 0 = gagal lokal (mis. ffmpeg tak ada), bukan penolakan FotoYu
		key := fmt.Sprintf("%d|%s", status, cut(msg, 120))
		q.mu.Lock()
		if q.rejectKey == key && now-q.rejectAt < 10*60000 {
			q.rejectList = append(q.rejectList, job.MD5)
		} else {
			q.rejectKey = key
			q.rejectList = []string{job.MD5}
		}
		q.rejectAt = now
		list := q.rejectList
		trip := len(list) >= 3
		q.mu.Unlock()
		if trip {
			for _, m := range list {
				q.store.Update(m, func(r *Record) { r.Status = "pending"; r.Stage = ""; r.Error = msg; r.NextAttempt = 0; r.Attempts = 0 })
			}
			q.mu.Lock()
			q.rejectList = nil
			if !q.paused {
				q.paused = true
				q.pauseReason = "setting"
				q.pauseMsg = "FotoYu menolak semua foto: " + msg
				q.emit.Log("error", fmt.Sprintf("Upload dijeda — FotoYu menolak beberapa foto dengan alasan sama: %q. Perbaiki lalu Lanjutkan.", msg))
			}
			q.mu.Unlock()
			return
		}
	}

	attempts := job.Attempts + 1
	giveUp := permanent || attempts > cfg.MaxRetries
	delay := int64(math.Min(60, math.Pow(2, float64(attempts)))) * 1000
	q.store.Update(job.MD5, func(r *Record) {
		r.Attempts = attempts
		r.Stage = ""
		r.Error = msg
		if giveUp {
			r.Status = "failed"
			r.NextAttempt = 0
		} else {
			r.Status = "pending"
			r.NextAttempt = nowMs() + delay
		}
	})
	if giveUp {
		q.emit.Log("error", "Gagal: "+job.Name+" — "+msg+failContext(job, ue, attempts, cfg.MaxRetries))
	} else {
		q.emit.Log("warn", fmt.Sprintf("Ulang dalam %d dtk: %s — %s", delay/1000, job.Name, msg)+failContext(job, ue, attempts, cfg.MaxRetries))
	}
}

// ---------- kontrol ----------

func (q *UploadQueue) Pause(reason string) {
	q.mu.Lock()
	q.paused = true
	q.pauseReason = reason
	q.pauseMsg = ""
	q.mu.Unlock()
	if reason == "manual" {
		q.cfg.Set(func(s *Settings) { s.AutoUpload = false })
	}
	q.emit.Emit("change", nil)
}

func (q *UploadQueue) Resume() {
	q.mu.Lock()
	q.paused = false
	q.pauseReason = ""
	q.pauseMsg = ""
	q.busyUntil = 0
	q.busyStreak = 0
	q.mu.Unlock()
	if q.cfg.Get().AutoUpload == false {
		q.cfg.Set(func(s *Settings) { s.AutoUpload = true })
	}
	q.emit.Emit("change", nil)
	q.wake()
}

func (q *UploadQueue) Paused() (bool, string, string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.paused, q.pauseReason, q.pauseMsg
}

func (q *UploadQueue) RetryFailed() {
	q.store.ForEach(func(r *Record) {
		if r.Status == "failed" {
			q.store.Update(r.MD5, func(x *Record) { x.Status = "pending"; x.Attempts = 0; x.Error = ""; x.NextAttempt = 0 })
		}
	})
	q.emit.Emit("change", nil)
	q.wake()
}

func (q *UploadQueue) SpeedPerMinute() int {
	cutoff := nowMs() - 60000
	q.statsMu.Lock()
	defer q.statsMu.Unlock()
	n := 0
	for _, t := range q.doneTimes {
		if t > cutoff {
			n++
		}
	}
	return n
}

// sentSample: satu unggahan selesai (waktu + ukuran hasil olah) — dipakai menghitung Mbps berjalan.
type sentSample struct {
	t     int64
	bytes int64
}

// Speed: ringkasan kecepatan berjalan untuk panel "progres" di Unggah — bukan grafik riwayat,
// cuma angka hidup (persis gaya MAS EDY: Mbps, foto/menit, sisa waktu, lama unggah sesi ini).
// Port ringkas dari speed() di queue.js.
type Speed struct {
	Mbps       float64 `json:"mbps"`
	PerMin     int     `json:"perMin"`
	EtaSec     int     `json:"etaSec"`     // -1 = belum bisa diperkirakan (data terlalu sedikit)
	ElapsedSec int     `json:"elapsedSec"` // 0 saat antrean kosong — sesi "berjalan" otomatis mulai/berhenti ikut antrean
}

// Speed menghitung mbps/perMin/eta/elapsed. `pending` = jumlah file pending+uploading saat ini
// (dihitung oleh pemanggil dari store.Counts(), supaya Speed tidak perlu query store sendiri).
func (q *UploadQueue) Speed(pending int) Speed {
	now := nowMs()
	q.statsMu.Lock()
	defer q.statsMu.Unlock()

	// --- aktif/tidaknya "sesi unggah" ---
	if pending > 0 {
		if q.activeSince == 0 {
			q.activeSince = now
		}
	} else {
		q.activeSince = 0
	}
	elapsedSec := 0
	if q.activeSince > 0 {
		elapsedSec = int((now - q.activeSince) / 1000)
	}

	// --- Mbps: jendela 30 dtk, rentang minimal 5 dtk supaya tak melonjak di awal ---
	cutoff30 := now - 30000
	var sumBytes int64
	earliest := now
	for _, s := range q.sentWindow {
		if s.t >= cutoff30 {
			sumBytes += s.bytes
			if s.t < earliest {
				earliest = s.t
			}
		}
	}
	spanSec := float64(now-earliest) / 1000
	if spanSec < 5 {
		spanSec = 5
	} else if spanSec > 30 {
		spanSec = 30
	}
	mbps := 0.0
	if sumBytes > 0 {
		mbps = float64(sumBytes) * 8 / spanSec / 1e6
	}

	// --- laju foto/detik dari 120 dtk terakhir (min. 3 sampel biar tak melompat-lompat) ---
	cutoff120 := now - 120000
	var recentDone []int64
	for _, t := range q.doneTimes {
		if t >= cutoff120 {
			recentDone = append(recentDone, t)
		}
	}
	rate := 0.0
	if len(recentDone) >= 3 {
		span := float64(now-recentDone[0]) / 1000
		if span < 30 {
			span = 30
		}
		rate = float64(len(recentDone)) / span
	}
	etaSec := -1
	if rate > 0 && pending > 0 {
		etaSec = int(math.Round(float64(pending) / rate))
	}

	perMin := 0
	cutoff60 := now - 60000
	for _, t := range q.doneTimes {
		if t > cutoff60 {
			perMin++
		}
	}

	return Speed{Mbps: math.Round(mbps*10) / 10, PerMin: perMin, EtaSec: etaSec, ElapsedSec: elapsedSec}
}

// FolderStat: progres unggah per folder sumber — dipakai saat lebih dari satu folder
// sumber aktif sekaligus (kompresi/unggah konkuren multi-folder).
type FolderStat struct {
	Folder  string `json:"folder"`
	Done    int    `json:"done"`
	Pending int    `json:"pending"` // termasuk yang sedang diunggah
	Failed  int    `json:"failed"`
	Total   int    `json:"total"`
	Pct     int    `json:"pct"`
}

// FolderProgress menghitung ringkasan per folder sumber (utama + tambahan) dari isi
// store saat ini — bukan dari penghitung terpisah, supaya selalu konsisten dengan
// angka Status.Counts secara keseluruhan.
func (q *UploadQueue) FolderProgress() []FolderStat {
	cfg := q.cfg.Get()
	roots := AllWatchFolders(cfg)
	stats := make([]FolderStat, len(roots))
	idx := map[string]int{}
	for i, r := range roots {
		stats[i] = FolderStat{Folder: r}
		idx[r] = i
	}
	q.store.ForEach(func(r *Record) {
		if !q.inCurrent(r, cfg) {
			return
		}
		i, ok := idx[RootFor(cfg, r.Path)]
		if !ok {
			return
		}
		switch r.Status {
		case "done":
			stats[i].Done++
		case "pending", "uploading":
			stats[i].Pending++
		case "failed":
			stats[i].Failed++
		}
	})
	for i := range stats {
		s := &stats[i]
		s.Total = s.Done + s.Pending + s.Failed
		if s.Total > 0 {
			s.Pct = int(math.Round(float64(s.Done) / float64(s.Total) * 100))
		}
	}
	return stats
}

// ---------- pindah ke "Sudah upload" ----------

func (q *UploadQueue) moveToArchive(filePath string, cfg Settings) string {
	dest := ArchiveDir(cfg, filePath)
	if dest == "" || filePath == "" || InArchive(cfg, filePath) {
		return ""
	}
	root := RootFor(cfg, filePath)
	if root == "" {
		root = cfg.WatchFolder
	}
	rel, err := filepath.Rel(root, filePath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		rel = filepath.Base(filePath)
	}
	dir := filepath.Join(dest, filepath.Dir(rel))
	ext := filepath.Ext(rel)
	base := strings.TrimSuffix(filepath.Base(rel), ext)
	if os.MkdirAll(dir, 0o755) != nil {
		return ""
	}
	for i := 1; i < 1000; i++ {
		name := base + ext
		if i > 1 {
			name = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		target := filepath.Join(dir, name)
		if fileExists(target) {
			continue
		}
		if os.Rename(filePath, target) == nil {
			return target
		}
		// beda disk: salin lalu hapus
		if copyFile(filePath, target) == nil {
			_ = os.Remove(filePath)
			return target
		}
		return ""
	}
	return ""
}

// ---------- util ----------

func md5File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func fileMTimeMs(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime().UnixMilli()
	}
	return nowMs()
}

func replaceExt(name, newExt string) string {
	return strings.TrimSuffix(name, filepath.Ext(name)) + newExt
}

func lutFor(cfg Settings, kind string) LUT {
	l := cfg.LUT
	if kind == "video" {
		l.File = l.Video
	} else {
		l.File = l.Photo
	}
	l.Enabled = l.File != ""
	return l
}

func pickPrice(cfg Settings, video bool) int {
	if video && cfg.Event.VideoPrice > 0 {
		return cfg.Event.VideoPrice
	}
	return cfg.Event.Price
}

func orientationOf(cfg Settings, job Record) string {
	return cfg.Video.Orientation
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func filterRecent(times []int64, now, window int64) []int64 {
	out := times[:0]
	for _, t := range times {
		if now-t < window {
			out = append(out, t)
		}
	}
	return out
}

func pick(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}
