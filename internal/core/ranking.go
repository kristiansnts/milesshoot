package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"fotonaik/internal/fotoyu"
)

// Ranking: peringkat akun berdasarkan jumlah daun di sebuah FotoTree. Murni informasi —
// tidak memengaruhi unggah atau uang masuk, jadi disengaja jadi yang terakhir dikerjakan.
//
// Port dari src/ranking.js. FotoYu mengurutkan daun dari yang TERBARU, jadi untuk rentang
// "hari ini"/"7 hari"/"30 hari" pemindaian berhenti begitu sampai ke daun yang lebih tua —
// tidak perlu menyisir seluruh riwayat FotoTree yang bisa berisi puluhan ribu daun.

const (
	rankPage     = 20 // sama dengan web FotoYu
	rankMaxPages = 5000
)

var rankRanges = map[string]string{"today": "Hari ini", "d7": "7 hari", "d30": "30 hari", "all": "Semua"}

func rankSince(rangeKey string) int64 {
	switch rangeKey {
	case "today":
		y, m, d := time.Now().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local).UnixMilli()
	case "d7":
		return time.Now().AddDate(0, 0, -7).UnixMilli()
	case "d30":
		return time.Now().AddDate(0, 0, -30).UnixMilli()
	}
	return 0
}

var rankKeyClean = regexp.MustCompile(`[^\w-]`)

func rankKey(treeID, rangeKey string) string {
	return rankKeyClean.ReplaceAllString(treeID, "") + "-" + rangeKey
}

type RankRow struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Photo    string `json:"photo"`
	Verified bool   `json:"verified"`
	Official bool   `json:"official"`
	Count    int    `json:"count"`
	Videos   int    `json:"videos"`
	LastAt   int64  `json:"last"`
}

type RankView struct {
	TreeID    string    `json:"treeId"`
	Name      string    `json:"name"`
	Range     string    `json:"range"`
	Scanned   int       `json:"scanned"`
	Total     int       `json:"total"`
	Done      bool      `json:"done"`
	Error     string    `json:"error"`
	UpdatedAt int64     `json:"updatedAt"`
	Cached    bool      `json:"cached"`
	Rows      []RankRow `json:"rows"`
	Accounts  int       `json:"accounts"`
}

type rankJob struct {
	mu        sync.Mutex
	key       string
	treeID    string
	name      string
	rangeKey  string
	scanned   int
	total     int
	rows      []RankRow
	done      bool
	cancelled bool
	errMsg    string
	updatedAt int64
}

func (j *rankJob) view() RankView {
	j.mu.Lock()
	defer j.mu.Unlock()
	rows := j.rows
	if len(rows) > 300 {
		rows = rows[:300]
	}
	return RankView{
		TreeID: j.treeID, Name: j.name, Range: j.rangeKey, Scanned: j.scanned, Total: j.total,
		Done: j.done, Error: j.errMsg, UpdatedAt: j.updatedAt, Rows: append([]RankRow{}, rows...), Accounts: len(j.rows),
	}
}

type RankingManager struct {
	mu   sync.Mutex
	dir  string
	fy   *fotoyu.Client
	emit Emitter
	jobs map[string]*rankJob
}

func NewRankingManager(dataDir string, fy *fotoyu.Client, emit Emitter) *RankingManager {
	return &RankingManager{dir: filepath.Join(dataDir, "ranking"), fy: fy, emit: emit, jobs: map[string]*rankJob{}}
}

func (m *RankingManager) Ranges() map[string]string { return rankRanges }

// Start memulai (atau mengembalikan job yang sedang berjalan untuk) treeID+range ini.
func (m *RankingManager) Start(treeID, name, rangeKey string) RankView {
	if _, ok := rankRanges[rangeKey]; !ok {
		rangeKey = "today"
	}
	key := rankKey(treeID, rangeKey)

	m.mu.Lock()
	if j, ok := m.jobs[key]; ok && !j.view().Done {
		m.mu.Unlock()
		return j.view()
	}
	j := &rankJob{key: key, treeID: treeID, name: name, rangeKey: rangeKey, rows: []RankRow{}}
	m.jobs[key] = j
	m.mu.Unlock()

	go m.run(j)
	return j.view()
}

// Status mengembalikan job yang sedang/sudah berjalan, atau hasil tersimpan dari sesi lalu.
func (m *RankingManager) Status(treeID, rangeKey string) (RankView, bool) {
	key := rankKey(treeID, rangeKey)
	m.mu.Lock()
	j, ok := m.jobs[key]
	m.mu.Unlock()
	if ok {
		return j.view(), true
	}
	if saved, ok := m.load(key); ok {
		saved.Done = true
		saved.Cached = true
		if len(saved.Rows) > 300 {
			saved.Accounts = len(saved.Rows)
			saved.Rows = saved.Rows[:300]
		} else {
			saved.Accounts = len(saved.Rows)
		}
		return saved, true
	}
	return RankView{}, false
}

func (m *RankingManager) Cancel(treeID, rangeKey string) {
	key := rankKey(treeID, rangeKey)
	m.mu.Lock()
	j, ok := m.jobs[key]
	m.mu.Unlock()
	if !ok {
		return
	}
	j.mu.Lock()
	if !j.done {
		j.cancelled = true
	}
	j.mu.Unlock()
}

func (m *RankingManager) run(j *rankJob) {
	since := rankSince(j.rangeKey)
	members := map[string]*RankRow{}
	order := []string{}
	cursor := ""

	if count, name, err := m.fy.TreeDetail(j.treeID); err == nil {
		j.mu.Lock()
		j.total = count
		if name != "" {
			j.name = name
		}
		j.mu.Unlock()
	}

	var runErr error
pages:
	for page := 0; page < rankMaxPages; page++ {
		j.mu.Lock()
		cancelled := j.cancelled
		j.mu.Unlock()
		if cancelled {
			runErr = errCancelled{}
			break
		}

		leaves, next, err := m.fy.TreeLeaves(j.treeID, rankPage, cursor)
		if err != nil {
			runErr = err
			break
		}

		stop := false
		for _, l := range leaves {
			t := parseFotoyuTime(l.CreatedAt)
			if since != 0 && t != 0 && t < since {
				stop = true
				break
			}
			id := l.Member.ID
			if id == "" {
				id = l.Member.Username
			}
			if id == "" {
				id = "?"
			}
			row, ok := members[id]
			if !ok {
				row = &RankRow{
					ID: id, Username: orElse(l.Member.Username, "?"), Photo: l.Member.Photo,
					Verified: l.Member.CreatorStatus == "verified", Official: l.Member.IsOfficial,
				}
				members[id] = row
				order = append(order, id)
			}
			row.Count++
			if l.ContentType == "video" {
				row.Videos++
			}
			if t > row.LastAt {
				row.LastAt = t
			}

			j.mu.Lock()
			j.scanned++
			j.mu.Unlock()
		}

		j.mu.Lock()
		j.rows = sortedRows(members, order)
		j.mu.Unlock()
		m.emit.Emit("ranking", j.view())

		if stop || next == "" || len(leaves) == 0 {
			break pages
		}
		cursor = next
		time.Sleep(120 * time.Millisecond) // pelan-pelan, jangan membebani server FotoYu
	}

	j.mu.Lock()
	j.done = true
	j.updatedAt = time.Now().UnixMilli()
	if runErr != nil {
		if _, ok := runErr.(errCancelled); ok {
			j.errMsg = "Dibatalkan"
		} else {
			j.errMsg = runErr.Error()
		}
	}
	j.mu.Unlock()

	// j.view() dipanggil SETELAH Unlock — ia mengunci j.mu sendiri di dalam,
	// dan sync.Mutex Go tidak reentrant. Memanggilnya sambil masih memegang
	// kunci yang sama menyebabkan goroutine mengunci dirinya sendiri selamanya
	// (persis bug yang dilaporkan: Ranking macet "Memuat..." tiap kali selesai).
	view := j.view()

	if runErr == nil {
		m.save(j.key, view)
		m.emit.Log("ok", "Ranking selesai: "+orElse(view.Name, j.treeID)+" ("+view.Range+") — "+itoaR(len(view.Rows))+" akun")
	}
	m.emit.Emit("ranking", view)
}

type errCancelled struct{}

func (errCancelled) Error() string { return "dibatalkan" }

func sortedRows(members map[string]*RankRow, order []string) []RankRow {
	out := make([]RankRow, 0, len(order))
	for _, id := range order {
		out = append(out, *members[id])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Username < out[j].Username
	})
	return out
}

// parseFotoyuTime: FotoYu mengirim ISO 8601 (RFC3339-ish). 0 bila tak terbaca.
func parseFotoyuTime(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// ---------- cache di disk ----------

func (m *RankingManager) load(key string) (RankView, bool) {
	b, err := os.ReadFile(filepath.Join(m.dir, key+".json"))
	if err != nil {
		return RankView{}, false
	}
	var v RankView
	if json.Unmarshal(b, &v) != nil {
		return RankView{}, false
	}
	return v, true
}

func (m *RankingManager) save(key string, v RankView) {
	_ = os.MkdirAll(m.dir, 0o755)
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	tmp := filepath.Join(m.dir, key+".json.tmp")
	final := filepath.Join(m.dir, key+".json")
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, final)
	}
}

func orElse(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func itoaR(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
