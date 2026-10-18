package kb

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

func (a *Module) organize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text     string `json:"text"`
		FolderID int64  `json:"folder_id"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" || utf8.RuneCountInString(in.Text) > 20000 {
		core.Fail(w, 400, "笔记须为 1–20000 字")
		return
	}
	rows, err := a.DB.Query("SELECT id,name FROM kb_folders ORDER BY sort,id")
	if err != nil {
		replyError(w, err)
		return
	}
	folders := map[int64]bool{}
	lines := []string{}
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			break
		}
		folders[id] = true
		lines = append(lines, fmt.Sprintf("%d %s", id, name))
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		replyError(w, err)
		return
	}
	if !folders[in.FolderID] {
		core.Fail(w, 400, "目录不存在")
		return
	}
	b, err := os.ReadFile(filepath.Join(a.Cfg.PromptDir, "知识库整理.md"))
	if err != nil {
		core.Fail(w, 500, "知识库整理提示词读取失败")
		return
	}
	prompt := core.FillPrompt(strings.ReplaceAll(string(b), "{{folders}}", strings.Join(lines, "\n")), nil)
	var out struct {
		Title    string   `json:"title"`
		Body     string   `json:"body"`
		Summary  string   `json:"summary"`
		Tags     []string `json:"tags"`
		FolderID int64    `json:"folder_id"`
	}
	ctx := core.WithAIActor(r.Context(), core.AIActor{UserID: core.CurrentUser(r).ID})
	err = a.AIJSON(core.WithFeature(ctx, "kb.organize"), a.Cfg.TextModel, prompt, core.JS(in), &out)
	if err != nil {
		core.Fail(w, 502, a.SafeAIError(err.Error()))
		return
	}
	// Recheck after the remote call: folders may have changed in the meantime.
	var n int
	if err = a.DB.QueryRow("SELECT count(*) FROM kb_folders WHERE id=?", out.FolderID).Scan(&n); err != nil {
		replyError(w, err)
		return
	}
	if n == 0 {
		out.FolderID = in.FolderID
	}
	if err = a.DB.QueryRow("SELECT count(*) FROM kb_folders WHERE id=?", out.FolderID).Scan(&n); err != nil {
		replyError(w, err)
		return
	}
	if n == 0 {
		core.Fail(w, 409, "目录刚被删除，请重新选择")
		return
	}
	out.Title = clip(strings.TrimSpace(out.Title), 100)
	out.Summary = clip(out.Summary, 200)
	out.Body = clip(out.Body, 100000)
	tags := []string{}
	seen := map[string]bool{}
	for _, tag := range out.Tags {
		tag = clip(strings.TrimSpace(tag), 20)
		if tag != "" && !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
		if len(tags) == 10 {
			break
		}
	}
	out.Tags = tags
	a.Audit(r, "kb.organize", "kb_folder", out.FolderID, nil)
	core.WriteJSON(w, 200, out)
}
