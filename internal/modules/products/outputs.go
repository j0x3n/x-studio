package products

import (
	"fmt"
	"os"
	"path/filepath"
	"xstudio/internal/core"
)

func (a *Module) outputs(id int64) core.Object {
	o := core.Object{"main34": []string{}, "main11": []string{}, "detail": []string{}, "white": "", "zip": ""}
	for key, sub := range map[string]string{"main34": "主图/3x4", "main11": "主图/1x1", "detail": "详情页"} {
		files, _ := filepath.Glob(filepath.Join(a.ListingDir(id), "导出", filepath.FromSlash(sub), "*.jpg"))
		urls := []string{}
		for _, f := range files {
			urls = append(urls, fmt.Sprintf("/files/listings/%d/导出/%s/%s", id, sub, filepath.Base(f)))
		}
		o[key] = urls
	}
	if _, e := os.Stat(filepath.Join(a.ListingDir(id), "导出", "白底.jpg")); e == nil {
		o["white"] = fmt.Sprintf("/files/listings/%d/导出/白底.jpg", id)
	}
	if _, e := os.Stat(filepath.Join(a.ListingDir(id), "导出.zip")); e == nil {
		o["zip"] = fmt.Sprintf("/api/listings/%d/download", id)
	}
	return o
}
