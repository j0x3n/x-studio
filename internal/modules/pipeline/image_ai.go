package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"xstudio/internal/core"
)

// Write beside the destination first so interrupted requests never leave a
// partially written image. Actual returned dimensions are preserved.
func writeAIFile(filename string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".ai-*.png")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filename)
}

func enhancementSections(doc string) map[string]string {
	out := map[string]string{}
	key := ""
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			key = ""
			for _, candidate := range []string{"通用要求", "高清化", "A1", "E1", "E2", "E3"} {
				if strings.HasPrefix(title, candidate) {
					key = candidate
					break
				}
			}
			continue
		}
		if key != "" {
			out[key] += line + "\n"
		}
	}
	return out
}
func enhancementPrompt(parts map[string]string, kind string, p Product) (string, error) {
	common, section := strings.TrimSpace(parts["通用要求"]), strings.TrimSpace(parts[kind])
	if common == "" || section == "" {
		return "", fmt.Errorf("补图.md 缺少通用要求或 %s 段落", photoCodeLabels(p.Category, kind))
	}
	fields := []string{"款名=" + p.Name}
	if p.Category != nil && p.Category.Name != "" {
		fields = append(fields, "品类="+p.Category.Name)
	}
	for _, key := range []string{"style", "variety", "pearl_type", "size", "colors", "highlights"} {
		if v, ok := p.Info[key]; ok {
			fields = append(fields, key+"="+strings.ReplaceAll(fmt.Sprint(v), "\n", " "))
		}
	}
	size, _ := p.Info["size"].(string)
	if strings.TrimSpace(size) == "" {
		size = "【待定】"
	}
	replacer := strings.NewReplacer("{info}", strings.Join(fields, "；"), "{size}", size)
	// 生图模型只吃这段文字，槽位等结构化信息不拼进来
	return core.FillPrompt(replacer.Replace(common+"\n\n"+section), p.Category), nil
}
