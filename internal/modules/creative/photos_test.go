package creative

import (
	"testing"
	"xstudio/internal/core"
)

// 重画、模特位：缺实拍的编号由同编号的 AI 补图顶上，排在原来的位置；实拍原图位只认实拍。
func TestSelectedPhotosUsesAIFillOnlyForRedrawAndModel(t *testing.T) {
	f := newFixture(t)
	insert := func(code string, aiGenerated bool) int64 {
		f.exec(t, "INSERT INTO assets(product_id,file,orig_name,width,height,code,ai_generated,source,created_by,created_at) VALUES(?,?,?,160,120,?,?,?,1,?)", f.productID, "原图/"+code+".png", code, code, aiGenerated, map[bool]string{true: "ai_generated", false: "original"}[aiGenerated], core.Now())
		return f.scalar(t, "SELECT id FROM assets WHERE product_id=? AND code=?", f.productID, code)
	}
	fill, worn := insert("A1_AI", true), insert("D1", false)
	product, err := f.a.PipelineProduct(f.listingID)
	if err != nil {
		t.Fatal(err)
	}
	pick := func(mode string) []int64 {
		tpl := testTemplate(mode)
		tpl.PhotoCodes = []string{"A1", "A2", "D1"}
		photos, err := f.a.selectedPhotos(product, tpl)
		if err != nil {
			return nil
		}
		ids := []int64{}
		for _, ph := range photos {
			ids = append(ids, ph.ID)
		}
		return ids
	}
	for _, mode := range []string{"redraw", "model"} {
		if got := pick(mode); len(got) != 2 || got[0] != fill || got[1] != worn {
			t.Fatal(mode, got)
		}
	}
	for _, mode := range []string{"photo", "pixel_lock"} {
		if got := pick(mode); len(got) != 1 || got[0] != worn {
			t.Fatal(mode, got)
		}
	}
	real := insert("A1", false)
	if got := pick("redraw"); len(got) != 2 || got[0] != real || got[1] != worn {
		t.Fatal("real A1 must win over A1_AI", got)
	}
}
