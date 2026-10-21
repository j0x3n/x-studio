package scenes

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"xstudio/internal/core"
)

type seed struct {
	Scene
	Key        string `json:"key"`
	Default    bool   `json:"default"`
	References []struct {
		Use  string `json:"use"`
		File string `json:"file"`
	} `json:"refs"`
}

func (module *Module) AfterMigrate() error {
	dir := filepath.Join(module.Cfg.TemplateDir, "生图场景")
	root, err := os.OpenRoot(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	seeds := []seed{}
	defaultCount := 0
	keys := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := root.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		var incoming seed
		if err = json.Unmarshal(raw, &incoming); err != nil {
			return fmt.Errorf("场景种子格式不正确：%s", entry.Name())
		}
		if incoming.Key == "" || keys[incoming.Key] {
			return fmt.Errorf("场景种子的 key 为空或重复：%s", entry.Name())
		}
		keys[incoming.Key] = true
		if err = validate(&incoming.Scene); err != nil {
			return fmt.Errorf("场景种子 %s：%w", entry.Name(), err)
		}
		if incoming.Default {
			defaultCount++
		}
		if defaultCount > 1 {
			return fmt.Errorf("只能有一个兜底场景")
		}
		incoming.ShopID = nil
		seeds = append(seeds, incoming)
	}
	for _, incoming := range seeds {
		var id int64
		if err = module.DB.QueryRow("SELECT coalesce((SELECT id FROM scenes WHERE builtin_key=?),0)", incoming.Key).Scan(&id); err != nil {
			return err
		}
		if id == 0 {
			if id, err = module.insertSeed(root, incoming); err != nil {
				return err
			}
		}
		if incoming.Default {
			if _, err = module.DB.Exec("INSERT INTO settings(key,value,updated_at) VALUES('scenes.default',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at", strconv.FormatInt(id, 10), core.Now()); err != nil {
				return err
			}
		}
	}
	return nil
}
func (module *Module) insertSeed(root *os.Root, incoming seed) (int64, error) {
	tx, err := module.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO scenes(name,categories,still,wear,light,props,note,builtin_key,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,0,?,?)`, incoming.Name, core.JS(incoming.Categories), incoming.Still, incoming.Wear, incoming.Light, incoming.Props, incoming.Note, incoming.Key, core.Now(), core.Now())
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	files := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, file := range files {
				_ = module.removeFile(file)
			}
		}
	}()
	counts := map[string]int{}
	for _, reference := range incoming.References {
		counts[reference.Use]++
		if !contains([]string{"still", "wear"}, reference.Use) || counts[reference.Use] > 4 || !fs.ValidPath(reference.File) || strings.ContainsAny(reference.File, "\\:") {
			return 0, fmt.Errorf("场景 %s 的参考图用途、数量或路径不正确", incoming.Key)
		}
		file, err := root.Open(reference.File)
		if err != nil {
			return 0, err
		}
		data, err := io.ReadAll(io.LimitReader(file, maxReferenceBytes+1))
		file.Close()
		if err != nil {
			return 0, err
		}
		ref, err := module.writeReference(id, data)
		if err != nil {
			return 0, err
		}
		files = append(files, ref.File)
		if _, err = tx.Exec("INSERT INTO scene_refs(scene_id,use,file,width,height,created_at) VALUES(?,?,?,?,?,?)", id, reference.Use, ref.File, ref.Width, ref.Height, core.Now()); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	committed = true
	module.AuditUser(0, "", "scene.create", "scene", id, core.Object{"name": incoming.Name, "builtin_key": incoming.Key})
	return id, nil
}
