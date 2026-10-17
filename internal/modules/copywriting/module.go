package copywriting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"xstudio/internal/core"
)

type Module struct {
	*core.Core
	ScanInterval time.Duration
	syncMu       sync.Mutex
	bgWG         sync.WaitGroup
}

func New(c *core.Core) *Module {
	return &Module{Core: c, ScanInterval: 30 * time.Second}
}

func (*Module) Name() string { return "copywriting" }

func (a *Module) Jobs() map[string]core.JobHandler {
	return map[string]core.JobHandler{
		"copy.generate": a.runGenerate,
		"copy.rewrite":  a.runRewrite,
	}
}

func (a *Module) Routes(r *core.Router) {
	r.Handle("GET /api/listings/{id}/copies", core.CopyView, a.listCopies)
	r.Handle("POST /api/listings/{id}/copies", core.CopyEdit, a.createCopy)
	r.Handle("POST /api/listings/{id}/copies/generate", core.CopyEdit, a.generateCopy)
	r.Handle("GET /api/copies/{id}", core.CopyView, a.getCopy)
	r.Handle("POST /api/copies/{id}/apply", core.CopyEdit, a.applyCopy)
	r.Handle("POST /api/copies/{id}/rewrite", core.CopyEdit, a.rewriteCopy)
	r.Handle("POST /api/copies/{id}/check", core.CopyView, a.checkCopy)

	r.Handle("GET /api/keywords", core.CopyView, a.listKeywords)
	r.Handle("POST /api/keywords", core.CopyEdit, a.createKeyword)
	r.Handle("POST /api/keywords/import", core.CopyEdit, a.importKeywords)
	r.Handle("PATCH /api/keywords/{id}", core.CopyEdit, a.patchKeyword)
	r.Handle("DELETE /api/keywords/{id}", core.CopyEdit, a.deleteKeyword)

	r.Handle("GET /api/shops/{id}/voice", core.CopyView, a.getVoice)
	r.Handle("PUT /api/shops/{id}/voice", core.CopyEdit, a.putVoice)

	a.bgWG.Add(1)
	go a.syncLoop()
	a.OnClose(func() { a.bgWG.Wait() })
}

func (a *Module) syncLoop() {
	defer a.bgWG.Done()
	interval := a.ScanInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-a.Context().Done():
			return
		case <-ticker.C:
			a.syncPipelineVersions()
		}
	}
}

func (a *Module) prompt(name string) (string, error) {
	b, e := os.ReadFile(filepath.Join(a.Cfg.PromptDir, name))
	if e != nil {
		return "", fmt.Errorf("缺少提示词 %s", name)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return "", fmt.Errorf("缺少提示词 %s", name)
	}
	return string(b), nil
}

func decodeOptional(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	b, e := io.ReadAll(r.Body)
	if e != nil {
		core.Fail(w, 400, "读取请求失败")
		return false
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return true
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if e := d.Decode(v); e != nil {
		core.Fail(w, 400, "JSON 格式或字段类型不正确")
		return false
	}
	if e := d.Decode(new(any)); e != io.EOF {
		core.Fail(w, 400, "只能提交一个 JSON 值")
		return false
	}
	return true
}
