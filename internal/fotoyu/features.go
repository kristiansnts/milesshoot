package fotoyu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Fitur FotoYu non-upload: cari FotoTree, rincian biaya, penyimpanan, ranking daun, hi-res.
// Port dari src/fotoyu-api.js (bagian trees/fee/storage/resend).

// ---------- FotoTree ----------

type TreeResult struct {
	ID       string   `json:"id"`
	TagID    string   `json:"tag_id"`
	Name     string   `json:"name"`
	Alias    string   `json:"alias"`
	Category string   `json:"category"`
	Location Location `json:"location"`
	IsBib    bool     `json:"is_bib"`
	Image    string   `json:"image"`
	Official bool     `json:"official"`
	Count    *int     `json:"count"`
	Host     string   `json:"host"`
}

type treeRaw struct {
	ID         string   `json:"id"`
	TagID      string   `json:"tag_id"`
	Name       string   `json:"name"`
	Alias      string   `json:"alias"`
	Category   string   `json:"category_name"`
	Location   Location `json:"location"`
	IsBib      bool     `json:"is_bib"`
	URL        string   `json:"url"`
	CountMem   *int     `json:"count_memory"`
	TotalMem   *int     `json:"total_memory"`
	MemCount   *int     `json:"memory_count"`
	IsOfficial bool     `json:"is_official"`
	IsVerified bool     `json:"is_verified"`
	Verified   bool     `json:"verified"`
	IsPartner  bool     `json:"is_partner"`
}

func (t treeRaw) official() bool {
	return t.IsOfficial || t.IsVerified || t.Verified || t.IsPartner
}
func firstCount(a ...*int) *int {
	for _, v := range a {
		if v != nil {
			return v
		}
	}
	return nil
}

func (c *Client) SearchTrees(name string, all bool) ([]TreeResult, error) {
	q := url.Values{"page": {"1"}, "limit": {"20"}, "name": {name}}
	if !all {
		q.Set("is_upload", "true")
	}
	env, err := c.call("GET", "/tree/v1/trees/search?"+q.Encode(), callOpts{auth: c.IsLoggedIn()})
	if err != nil {
		return nil, err
	}
	var raw []treeRaw
	_ = json.Unmarshal(env.Result, &raw)
	out := make([]TreeResult, 0, len(raw))
	for _, t := range raw {
		out = append(out, TreeResult{
			ID: t.ID, TagID: t.TagID, Name: t.Name, Alias: t.Alias, Category: t.Category,
			Location: t.Location, IsBib: t.IsBib, Image: t.URL, Official: t.official(),
			Count: firstCount(t.CountMem, t.TotalMem, t.MemCount),
		})
	}
	// resmi dulu, lalu foto terbanyak
	sortTrees(out)
	return out, nil
}

func sortTrees(ts []TreeResult) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && less(ts[j], ts[j-1]); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}
func less(a, b TreeResult) bool {
	if a.Official != b.Official {
		return a.Official
	}
	ca, cb := 0, 0
	if a.Count != nil {
		ca = *a.Count
	}
	if b.Count != nil {
		cb = *b.Count
	}
	return ca > cb
}

// ---------- rincian biaya ----------

type FeeItem struct {
	Label      string  `json:"label"`
	Nominal    int     `json:"nominal"`
	Percentage float64 `json:"percentage"`
}
type FeeResult struct {
	Buyer  int       `json:"buyer"`
	Net    int       `json:"net"`
	NetPct float64   `json:"netPct"`
	Items  []FeeItem `json:"items"`
}

type feeAmt struct {
	Nominal    int     `json:"nominal"`
	Percentage float64 `json:"percentage"`
}
type feeWeb struct {
	Web feeAmt `json:"web"`
}

func (c *Client) Fee(price int, tagID string) (FeeResult, error) {
	q := url.Values{"price": {strconv.Itoa(price)}}
	if tagID != "" {
		q.Set("tag_id", tagID)
	}
	env, err := c.call("GET", "/payment/v2/transactions/fee?"+q.Encode(), callOpts{auth: true})
	if err != nil {
		return FeeResult{}, err
	}
	var x struct {
		Price struct {
			Web struct {
				Final      feeAmt `json:"final"`
				NetCreator feeAmt `json:"net_creator"`
			} `json:"web"`
		} `json:"price"`
		FotoyuFee   feeWeb `json:"fotoyu_fee"`
		HostFee     feeWeb `json:"host_fee"`
		PlatformFee feeWeb `json:"platform_fee"`
		ServiceFee  feeWeb `json:"service_fee"`
		TaxFee      feeWeb `json:"tax_fee"`
	}
	_ = json.Unmarshal(env.Result, &x)
	res := FeeResult{Buyer: x.Price.Web.Final.Nominal, Net: x.Price.Web.NetCreator.Nominal, NetPct: x.Price.Web.NetCreator.Percentage}
	if res.Buyer == 0 {
		res.Buyer = price
	}
	for _, it := range []struct {
		label string
		w     feeWeb
	}{
		{"Biaya FotoYu", x.FotoyuFee}, {"Biaya host FotoTree", x.HostFee}, {"Biaya platform", x.PlatformFee},
		{"Biaya layanan", x.ServiceFee}, {"Pajak", x.TaxFee},
	} {
		if it.w.Web.Nominal != 0 || it.w.Web.Percentage != 0 {
			res.Items = append(res.Items, FeeItem{Label: it.label, Nominal: it.w.Web.Nominal, Percentage: it.w.Web.Percentage})
		}
	}
	return res, nil
}

// ---------- penyimpanan ----------

type StorageInfo struct {
	Used      int64 `json:"used"`
	Remaining int64 `json:"remaining"`
	Limit     int64 `json:"limit"`
	Files     int64 `json:"files"`
}

func (c *Client) Storage() (StorageInfo, error) {
	env, err := c.call("GET", "/gs/v1/creators/storage", callOpts{auth: true})
	if err != nil {
		return StorageInfo{}, err
	}
	var s struct {
		Used      int64 `json:"used_storage"`
		Remaining int64 `json:"remaining_storage"`
		Limit     int64 `json:"limit_storage"`
		Bonus     int64 `json:"bonus_storage"`
		Files     int64 `json:"total_files"`
	}
	_ = json.Unmarshal(env.Result, &s)
	limit := s.Limit
	if s.Bonus > limit {
		limit = s.Bonus
	}
	if limit == 0 {
		limit = s.Used + s.Remaining
	}
	return StorageInfo{Used: s.Used, Remaining: s.Remaining, Limit: limit, Files: s.Files}, nil
}

// ---------- ranking (daun per akun di sebuah FotoTree) ----------

// TreeDetail: dipakai ranking untuk tahu total daun (count_memory) & nama FotoTree.
func (c *Client) TreeDetail(treeID string) (count int, name string, err error) {
	q := url.Values{"tree_id": {treeID}}
	env, e := c.call("GET", "/tree/v1/trees/detail?"+q.Encode(), callOpts{auth: c.IsLoggedIn()})
	if e != nil {
		return 0, "", e
	}
	var x struct {
		Name     string `json:"name"`
		CountMem *int   `json:"count_memory"`
	}
	_ = json.Unmarshal(env.Result, &x)
	if x.CountMem != nil {
		count = *x.CountMem
	}
	return count, x.Name, nil
}

type LeafMember struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Photo         string `json:"photo"`
	CreatorStatus string `json:"creator_status"`
	IsOfficial    bool   `json:"is_official"`
}

type Leaf struct {
	CreatedAt   string     `json:"created_at"`
	ContentType string     `json:"content_type"` // photo|video
	Member      LeafMember `json:"member"`
}

// TreeLeaves: daun (foto/video terjual/dipajang) di sebuah FotoTree, terbaru dulu, berhalaman.
func (c *Client) TreeLeaves(treeID string, limit int, cursor string) (items []Leaf, next string, err error) {
	if limit <= 0 {
		limit = 20
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}, "tree_id": {treeID}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	env, e := c.call("GET", "/tree/v1/leaves?"+q.Encode(), callOpts{auth: c.IsLoggedIn()})
	if e != nil {
		return nil, "", e
	}
	var arr []Leaf
	_ = json.Unmarshal(env.Result, &arr)
	return arr, env.Paging.NextCursor, nil
}

// ---------- hi-res: kirim ulang file asli untuk yang terjual ----------

type ResendItem struct {
	ContentID   string `json:"content_id"`
	ContentType string `json:"content_type"`
	Signature   string `json:"signature"` // MD5 file asli
	Title       string `json:"title"`     // nama file
}

func (c *Client) ListResend() ([]ResendItem, error) {
	var out []ResendItem
	for page := 1; page <= 50; page++ {
		env, err := c.call("GET", fmt.Sprintf("/gs/v5/creations?page=%d&limit=50&filter=resend&sort_by=title&sort_dir=desc", page), callOpts{auth: true})
		if err != nil {
			return nil, err
		}
		var data struct {
			Data []ResendItem `json:"data"`
		}
		_ = json.Unmarshal(env.Result, &data)
		out = append(out, data.Data...)
		if len(data.Data) < 50 {
			break
		}
	}
	return out, nil
}

type ResendFile struct {
	Path        string
	Size        int64
	Width       int
	Height      int
	Extension   string
	Mime        string
	DurationSec float64
}

func (c *Client) SendResend(item ResendItem, file ResendFile) (Result, error) {
	acct, err := c.curAcct()
	if err != nil {
		return Result{}, err
	}
	if item.ContentType == "video" {
		ms := int(file.DurationSec * 1000)
		env, err := c.call("POST", "/gs/v1/creations/videos/resend/link", callOpts{auth: true, body: map[string]any{
			"content_id": item.ContentID,
			"video":      map[string]any{"duration": ms, "extension": file.Extension, "height": file.Height, "size": file.Size, "width": file.Width},
		}})
		if err != nil {
			return Result{}, err
		}
		link := resendLink(env.Result)
		if link == "" {
			return Result{}, errf(500, "FotoYu tidak memberi link hi-res video")
		}
		if err := c.putFile(link, file); err != nil {
			return Result{}, err
		}
		exif, _ := json.Marshal(map[string]any{"duration": ms})
		_, err = c.call("POST", "/gs/v1/creations/videos/resend", callOpts{auth: true, body: map[string]any{
			"content": link, "content_id": item.ContentID, "exif": string(exif),
			"height": file.Height, "size": file.Size, "width": file.Width,
		}})
		return Result{Account: acct.User.Username}, err
	}

	env, err := c.call("POST", "/gs/v2/creations/resend/link", callOpts{auth: true, body: map[string]any{
		"content":    map[string]any{"size": file.Size, "height": file.Height, "width": file.Width, "extension": file.Extension},
		"content_id": item.ContentID,
	}})
	if err != nil {
		return Result{}, err
	}
	link := resendLink(env.Result)
	if link == "" {
		return Result{}, errf(500, "FotoYu tidak memberi link hi-res")
	}
	if err := c.putFile(link, file); err != nil {
		return Result{}, err
	}
	_, err = c.call("POST", "/gs/v2/creations/resend", callOpts{auth: true, body: map[string]any{
		"content": link, "content_id": item.ContentID, "height": file.Height, "width": file.Width, "size": file.Size,
	}})
	return Result{Account: acct.User.Username}, err
}

func resendLink(raw json.RawMessage) string {
	var r struct {
		ResendLink string `json:"resend_link"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.ResendLink
}

// putFile mengalirkan file asli apa adanya ke link penyimpanan (≥40 KB/dtk).
func (c *Client) putFile(link string, file ResendFile) error {
	f, err := os.Open(file.Path)
	if err != nil {
		return errf(0, "Gagal membuka file hi-res (%v)", err)
	}
	defer f.Close()
	timeout := time.Duration(120+file.Size/(40*1024)) * time.Second
	req, err := http.NewRequest("PUT", link, f)
	if err != nil {
		return errf(0, "link hi-res tidak valid")
	}
	req.ContentLength = file.Size
	req.Header.Set("Content-Type", file.Mime)
	req.Header.Set("x-amz-meta-imagewidth", strconv.Itoa(file.Width))
	req.Header.Set("x-amz-meta-imageheight", strconv.Itoa(file.Height))
	cl := &http.Client{Timeout: timeout}
	res, err := cl.Do(req)
	if err != nil {
		return errf(0, "Gagal mengirim file hi-res (%v)", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errf(res.StatusCode, "Penyimpanan FotoYu menolak file hi-res (%d)", res.StatusCode)
	}
	return nil
}
