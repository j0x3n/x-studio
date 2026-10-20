package assets

import (
	"fmt"
	"io/fs"
	"strings"
	"xstudio/internal/core"
)

type Asset struct {
	core.Photo
	Title        string `json:"title"`
	Note         string `json:"note"`
	Archived     bool   `json:"archived"`
	UpdatedAt    string `json:"updated_at"`
	PHash        string `json:"phash"`
	FolderID     *int64 `json:"folder_id"`
	Product      Object `json:"product"`
	Effective    string `json:"effective_source"`
	ThumbURL     string `json:"thumb_url"`
	VersionCount int    `json:"version_count"`
}

const assetColumns = core.PhotoColumns + ",title,note,archived,updated_at,phash,folder_id"

type extraScanner struct {
	core.Scanner
	extra []any
}

func (s extraScanner) Scan(v ...any) error { return s.Scanner.Scan(append(v, s.extra...)...) }
func scanAsset(s core.Scanner) (p Asset, err error) {
	p.Photo, err = core.ScanPhoto(extraScanner{s, []any{&p.Title, &p.Note, &p.Archived, &p.UpdatedAt, &p.PHash, &p.FolderID}})
	return
}
func (a *Module) asset(id int64) (Asset, error) {
	return scanAsset(a.DB.QueryRow("SELECT "+assetColumns+" FROM assets WHERE id=?", id))
}
func (a *Module) readAssets(query string, args ...any) ([]Asset, error) {
	rows, err := a.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Asset{}
	for rows.Next() {
		p, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (a *Module) ensureHash(p *Asset) error {
	if p.PHash != "" {
		return nil
	}
	// Stored paths originate from upload/processing. Check again before reading.
	if !validAssetFile(p.File) {
		return fmt.Errorf("素材文件路径不合法")
	}
	hash, err := a.calculateHash(*p)
	if err != nil {
		return fmt.Errorf("读取素材图片失败：%w", err)
	}
	p.PHash = hash
	_, err = a.DB.Exec("UPDATE assets SET phash=? WHERE id=? AND product_id=? AND file=? AND phash=''", p.PHash, p.ID, p.ProductID, p.File)
	return err
}
func validAssetFile(name string) bool { return fs.ValidPath(name) && !strings.ContainsAny(name, "\\:") }

func rootOf(p Asset, byID map[int64]Asset) int64 {
	seen := map[int64]bool{}
	for p.ParentID != nil && !seen[p.ID] {
		seen[p.ID] = true
		parent, ok := byID[*p.ParentID]
		if !ok {
			break
		}
		p = parent
	}
	return p.ID
}
func (a *Module) decorate(p Asset, u core.User, chain []Asset) Asset {
	var name string
	_ = a.DB.QueryRow("SELECT name FROM products WHERE id=?", p.ProductID).Scan(&name)
	p.Product = Object{"id": p.ProductID, "name": name}
	p.Effective = p.EffectiveSource()
	p.ThumbURL = p.URL + "?w=240"
	byID := map[int64]Asset{}
	for _, v := range chain {
		byID[v.ID] = v
	}
	root := rootOf(p, byID)
	for _, v := range chain {
		if rootOf(v, byID) == root {
			p.VersionCount++
		}
	}
	return p
}
func normalizeTags(tags []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" && !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}
