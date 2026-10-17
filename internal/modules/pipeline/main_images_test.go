package pipeline

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanMain(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		if err := saveJPEG(filepath.Join(dir, name), image.NewNRGBA(image.Rect(0, 0, 8, 8)), 92); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		imgs []Object
		want [][]string
	}{
		{"photo missing f", []Object{{"n": 1, "layout": "photo"}}, nil},
		{"photo null f", []Object{{"n": 1, "layout": "photo", "f": nil}}, nil},
		{"photo missing file", []Object{{"n": 1, "layout": "photo", "f": "missing.jpg"}}, nil},
		{"ruler missing f", []Object{{"n": 1, "layout": "ruler"}}, nil},
		{"ruler missing file", []Object{{"n": 1, "layout": "ruler", "f": "missing.jpg"}}, nil},
		{"sizes one survives", []Object{{"n": 1, "layout": "sizes", "items": []Object{{"f": "a.jpg"}, {"f": "missing.jpg"}}}}, nil},
		{"variants one survives", []Object{{"n": 1, "layout": "variants", "items": []Object{{"f": "a.jpg"}, {"f": "missing.jpg"}}}}, nil},
		{"variants empty", []Object{{"n": 1, "layout": "variants", "items": []Object{{"f": "missing.jpg"}}}}, nil},
		{"variants absent items", []Object{{"n": 1, "layout": "variants"}}, nil},
		{"all removed", []Object{{"n": 1, "layout": "photo"}, {"n": 2, "layout": "ruler", "f": "missing.jpg"}, {"n": 3, "layout": "sizes", "items": []Object{{"f": "a.jpg"}}}}, nil},
		{"renumber in input order", []Object{{"n": 8, "layout": "photo", "f": "a.jpg"}, {"n": 2, "layout": "photo", "f": "missing.jpg"}, {"n": 5, "layout": "ruler", "f": "b.jpg"}, {"n": 9, "f": "c.jpg"}}, [][]string{{"a.jpg"}, {"b.jpg"}, {"c.jpg"}}},
		{"sizes remove only missing items", []Object{{"n": 4, "layout": "sizes", "items": []Object{{"f": "a.jpg", "t": "小"}, {"f": "missing.jpg"}, {"f": "b.jpg", "t": "大"}, {"need": "缺 f"}}}}, [][]string{{"a.jpg", "b.jpg"}}},
		{"variants remove unsafe items", []Object{{"n": 5, "layout": "variants", "items": []Object{{"f": "a.jpg"}, {"f": "../a.jpg"}, {"f": "b.jpg"}, {"f": `..\b.jpg`}, {"f": "c.jpg"}}}}, [][]string{{"a.jpg", "b.jpg", "c.jpg"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := object(js(Object{"name": "保留款名", "imgs": tc.imgs}))
			before := js(main)
			got := cleanMain(main, dir)
			imgs := got["imgs"].([]any)
			if len(imgs) != len(tc.want) || got["name"] != "保留款名" {
				t.Fatalf("got %s", js(got))
			}
			for i, v := range imgs {
				m := v.(Object)
				if m["n"] != float64(i+1) {
					t.Fatal("noncontinuous n", m)
				}
				files := []string{}
				if items, ok := m["items"].([]any); ok {
					for _, item := range items {
						files = append(files, item.(Object)["f"].(string))
					}
				} else {
					files = append(files, m["f"].(string))
				}
				if js(files) != js(tc.want[i]) {
					t.Fatal(files, tc.want[i])
				}
			}
			if js(main) != before {
				t.Fatal("cleanMain mutated input")
			}
		})
	}
}

func TestPatchProductCleansMain(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	id := f.product(t)
	dir := filepath.Join(f.a.Dir(id), "照片")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jpg", "b.jpg"} {
		if err := saveJPEG(filepath.Join(dir, name), image.NewNRGBA(image.Rect(0, 0, 8, 8)), 92); err != nil {
			t.Fatal(err)
		}
	}
	main := Object{"imgs": []Object{{"n": 1, "layout": "photo", "f": "missing.jpg"}, {"n": 4, "layout": "ruler", "f": "a.jpg"}, {"n": 5, "layout": "variants", "items": []Object{{"f": "a.jpg"}, {"f": "missing.jpg"}}}, {"n": 6, "layout": "sizes", "items": []Object{{"f": "a.jpg"}, {"f": "b.jpg"}, {"f": "missing.jpg"}}}}}
	f.req(t, "PATCH", "/api/listings/"+intString(id), Object{"main": main}, 200)
	p, err := f.a.product(id)
	if err != nil {
		t.Fatal(err)
	}
	imgs := p.Main["imgs"].([]any)
	if len(imgs) != 2 || imgs[0].(Object)["n"] != 1.0 || imgs[1].(Object)["n"] != 2.0 || len(imgs[1].(Object)["items"].([]any)) != 2 {
		t.Fatal(js(p.Main))
	}
	f.req(t, "PATCH", "/api/listings/"+intString(id), Object{"main": Object{"imgs": []Object{{"n": 1, "layout": "photo"}}}}, 200)
	p, _ = f.a.product(id)
	if len(p.Main["imgs"].([]any)) != 0 {
		t.Fatal("empty main not saved")
	}
}

func TestRenderNoUsableMain(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	id := f.product(t)
	p, _ := f.a.product(id)
	p.Main = object(`{"imgs":[{"n":1,"layout":"photo","f":"missing.jpg"}]}`)
	err := f.a.render(testActor(context.Background()), p)
	if err == nil || err.Error() != "没有可用的主图照片，至少需要一张上耳图或场景图" {
		t.Fatal(err)
	}
	if f.a.browser.ctx != nil {
		t.Fatal("should fail before Chrome startup")
	}
	p, _ = f.a.product(id)
	if len(p.Main["imgs"].([]any)) != 0 {
		t.Fatal("cleaned main not persisted")
	}
}

func TestRenderRemovesDeletedMainPhotoAndRenumbers(t *testing.T) {
	chrome := ChromePath(os.Getenv("CHROME_PATH"))
	if chrome == "" {
		t.Skip("未安装 Chrome，跳过真实渲染测试")
	}
	t.Setenv("CHROME_NO_SANDBOX", "1")
	f := newFixture(t, func(c *Config) { c.ChromePath = chrome })
	f.login(t, "admin")
	id := f.product(t)
	dir := filepath.Join(f.a.Dir(id), "照片")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]color.NRGBA{"D4.jpg": {220, 40, 40, 255}, "E1.jpg": {40, 220, 40, 255}, "B1.jpg": {40, 40, 220, 255}} {
		im := image.NewNRGBA(image.Rect(0, 0, 80, 80))
		for y := 0; y < 80; y++ {
			for x := 0; x < 80; x++ {
				im.SetNRGBA(x, y, c)
			}
		}
		if err := saveJPEG(filepath.Join(dir, name), im, 92); err != nil {
			t.Fatal(err)
		}
	}
	f.req(t, "PATCH", "/api/listings/"+intString(id), Object{"main": Object{"imgs": []Object{{"n": 1, "f": "D4.jpg"}, {"n": 2, "f": "E1.jpg"}, {"n": 3, "f": "B1.jpg"}}}}, 200)
	if err := os.Remove(filepath.Join(dir, "E1.jpg")); err != nil {
		t.Fatal(err)
	}
	p, _ := f.a.product(id)
	if err := f.a.render(testActor(context.Background()), p); err != nil {
		t.Fatal(err)
	}
	p, _ = f.a.product(id)
	imgs := p.Main["imgs"].([]any)
	if len(imgs) != 2 || imgs[0].(Object)["f"] != "D4.jpg" || imgs[1].(Object)["f"] != "B1.jpg" || imgs[1].(Object)["n"] != 2.0 {
		t.Fatal(js(p.Main))
	}
	for _, ratio := range []string{"3x4", "1x1"} {
		files, err := os.ReadDir(filepath.Join(f.a.ListingDir(id), "导出", "主图", ratio))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 2 || files[0].Name() != "01.jpg" || files[1].Name() != "02.jpg" {
			t.Fatal(files)
		}
		file, err := os.Open(filepath.Join(f.a.ListingDir(id), "导出", "主图", ratio, "02.jpg"))
		if err != nil {
			t.Fatal(err)
		}
		im, err := jpeg.Decode(file)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := im.At(im.Bounds().Dx()/2, im.Bounds().Dy()/2).RGBA()
		if b <= r*3 || b <= g*3 {
			t.Fatal("second screenshot did not use renumbered B1 photo", r, g, b)
		}
	}
}
