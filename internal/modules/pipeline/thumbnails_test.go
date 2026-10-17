package pipeline

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func getJPEG(t *testing.T, f *fixture, path string, width, height int) []byte {
	t.Helper()
	res, err := f.client.Get(f.s.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("GET %s: %d %s: %s", path, res.StatusCode, res.Header.Get("Content-Type"), b)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != width || cfg.Height != height {
		t.Fatalf("thumbnail %dx%d, want %dx%d", cfg.Width, cfg.Height, width, height)
	}
	return b
}

func TestThumbnailsDimensionsCacheAndOriginal(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	id := f.product(t)
	original := photoBytes(t, 1200, 600)
	up := f.upload(t, id, "large.png", original, 201)
	file := up["photos"].([]any)[0].(Object)["file"].(string)
	url := fmt.Sprintf("/files/products/%d/%s", id, file)
	for _, size := range []int{240, 480, 960} {
		b := getJPEG(t, f, fmt.Sprintf("%s?w=%d", url, size), size, size/2)
		cacheName := filepath.Join(f.a.Dir(id), ".thumbs", fmt.Sprint(size), filepath.FromSlash(file))
		cached, err := os.ReadFile(cacheName)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, cached) {
			t.Fatal("response did not match disk cache")
		}
		// Check JPEG quality 82 as well as dimensions.
		decoded, _, err := image.Decode(bytes.NewReader(original))
		if err != nil {
			t.Fatal(err)
		}
		var expected bytes.Buffer
		if err = jpeg.Encode(&expected, resize(decoded, size), &jpeg.Options{Quality: 82}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, expected.Bytes()) {
			t.Fatal("thumbnail encoding differs from JPEG quality 82")
		}
	}
	res, err := f.client.Get(f.s.URL + url)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(raw, original) {
		t.Fatal("raw download changed")
	}
	listing := f.req(t, "GET", "/api/products", nil, 200)
	if listing["items"].([]any)[0].(Object)["cover_url"] != url+"?w=240" {
		t.Fatal("cover_url missing thumbnail width", listing)
	}
	cacheName := filepath.Join(f.a.Dir(id), ".thumbs", "240", filepath.FromSlash(file))
	sourceName := filepath.Join(f.a.Dir(id), filepath.FromSlash(file))
	marker := time.Now().Add(time.Minute).Truncate(time.Second)
	if err = os.Chtimes(cacheName, marker, marker); err != nil {
		t.Fatal(err)
	}
	getJPEG(t, f, url+"?w=240", 240, 120)
	stat, err := os.Stat(cacheName)
	if err != nil {
		t.Fatal(err)
	}
	if !stat.ModTime().Equal(marker) {
		t.Fatal("cache hit regenerated the image")
	}
	updated := photoBytes(t, 600, 1200)
	if err = os.WriteFile(sourceName, updated, 0600); err != nil {
		t.Fatal(err)
	}
	sourceTime := marker.Add(time.Minute)
	if err = os.Chtimes(sourceName, sourceTime, sourceTime); err != nil {
		t.Fatal(err)
	}
	regenerated := getJPEG(t, f, url+"?w=240", 120, 240)
	cached, err := os.ReadFile(cacheName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(regenerated, cached) {
		t.Fatal("stale cache was not replaced")
	}
	stat, err = os.Stat(cacheName)
	if err != nil {
		t.Fatal(err)
	}
	cacheTime := stat.ModTime()
	if cacheTime.Before(sourceTime) {
		t.Fatal("cache remains older than source")
	}
	getJPEG(t, f, url+"?w=240", 120, 240)
	stat, _ = os.Stat(cacheName)
	if !stat.ModTime().Equal(cacheTime) {
		t.Fatal("future-dated source repeatedly regenerated cache")
	}
	if err = os.Remove(sourceName); err != nil {
		t.Fatal(err)
	}
	f.req(t, "GET", url+"?w=240", nil, 404)
}

func TestThumbnailsNoUpscaleAndExportPaths(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	id := f.product(t)
	up := f.upload(t, id, "small.png", photoBytes(t, 64, 32), 201)
	file := up["photos"].([]any)[0].(Object)["file"].(string)
	getJPEG(t, f, fmt.Sprintf("/files/products/%d/%s?w=960", id, file), 64, 32)
	for _, name := range []string{"照片/A1.jpg", "导出/主图/3x4/01.jpg", "导出/详情页/01.jpg"} {
		root := f.a.Dir(id)
		kind := "products"
		if strings.HasPrefix(name, "导出/") {
			root = f.a.ListingDir(id)
			kind = "listings"
		}
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, photoBytes(t, 800, 1600), 0600); err != nil {
			t.Fatal(err)
		}
		getJPEG(t, f, fmt.Sprintf("/files/%s/%d/%s?w=480", kind, id, name), 240, 480)
		if _, err := os.Stat(filepath.Join(root, ".thumbs", "480", filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	// The thumbnail should follow EXIF orientation, like the original in a browser.
	up = f.upload(t, id, "rotated.jpg", jpegExif(t, image.NewNRGBA(image.Rect(0, 0, 400, 800)), 6, false), 201)
	file = up["photos"].([]any)[0].(Object)["file"].(string)
	getJPEG(t, f, fmt.Sprintf("/files/products/%d/%s?w=240", id, file), 240, 120)
}

func TestThumbnailValidationAndPermissions(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	id := f.product(t)
	up := f.upload(t, id, "photo.png", photoBytes(t, 32, 64), 201)
	file := up["photos"].([]any)[0].(Object)["file"].(string)
	url := fmt.Sprintf("/files/products/%d/%s", id, file)
	for _, query := range []string{"w=", "w=0", "w=241", "w=-240", "w=0240", "w=240.0", "w=abc", "w=240&w=480", "w=%ZZ"} {
		f.req(t, "GET", url+"?"+query, nil, 400)
	}
	for _, name := range []string{`原图/..%5c..%5capp.db`, `照片/C:%5cWindows%5cfile.jpg`, `导出/..%5c..%5coutside.jpg`} {
		kind := "products"
		if strings.HasPrefix(name, "导出/") {
			kind = "listings"
		}
		f.req(t, "GET", fmt.Sprintf("/files/%s/%d/%s?w=240", kind, id, name), nil, 400)
	}
	f.req(t, "GET", fmt.Sprintf("/files/products/%d/.thumbs/240/%s?w=240", id, file), nil, 404)
	text := filepath.Join(f.a.ListingDir(id), "导出", "标题和SKU.txt")
	if err := os.MkdirAll(filepath.Dir(text), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(text, []byte("不是照片"), 0600); err != nil {
		t.Fatal(err)
	}
	f.req(t, "GET", fmt.Sprintf("/files/listings/%d/导出/标题和SKU.txt?w=240", id), nil, 400)
	res, err := http.Get(f.s.URL + url + "?w=240")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("thumbnail bypassed login")
	}
	// Staff retain the same read access to other owners' photos as raw files.
	f.req(t, "POST", "/api/users", Object{"username": "staff", "password": "password123", "role_id": 2, "shop_ids": []int64{1}}, 201)
	f.login(t, "staff")
	getJPEG(t, f, url+"?w=240", 32, 64)
	entries, err := os.ReadDir(filepath.Join(f.a.Dir(id), ".thumbs", "240", "原图"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatal("temporary cache file leaked")
		}
	}
}
