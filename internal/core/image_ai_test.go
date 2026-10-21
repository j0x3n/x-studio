package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func imageTestCore(t *testing.T, handler http.HandlerFunc) *Core {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	a, e := New(Config{DataDir: t.TempDir(), SessionSecret: strings.Repeat("s", 32), AIBaseURL: server.URL, AIKey: "private-key", ImageModel: "existing-model", ImageTimeout: time.Second}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	_, e = a.DB.Exec(coreSchema + `CREATE TABLE roles(id INTEGER PRIMARY KEY,key TEXT,name TEXT,builtin INTEGER,scope TEXT,perms TEXT,created_at TEXT);
 CREATE TABLE users(id INTEGER PRIMARY KEY,username TEXT,password_hash BLOB,display_name TEXT,role_id INTEGER,created_at TEXT,disabled INTEGER,version INTEGER);
 INSERT INTO roles VALUES(1,'admin','admin',1,'all','[]','');INSERT INTO users VALUES(1,'user',NULL,'user',1,'',0,1);
 INSERT INTO quotas(user_id,month_images) VALUES(1,1);`)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func imageTestPNG(t *testing.T, sizes ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	width, height := 1024, 1024
	if len(sizes) > 0 {
		_, _ = fmt.Sscanf(sizes[0], "%dx%d", &width, &height)
	}
	if e := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, width, height))); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func imageTestActor() context.Context {
	return WithAIActor(context.Background(), AIActor{UserID: 1, JobID: 9, Feature: "model_create"})
}

func TestEditImageFitsFirstPortraitReferenceToLandscape(t *testing.T) {
	portrait := image.NewNRGBA(image.Rect(0, 0, 24, 36))
	other := image.NewNRGBA(image.Rect(0, 0, 12, 18))
	refs := []image.Image{portrait, other}
	a := imageTestCore(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		for index, header := range r.MultipartForm.File["image"] {
			file, err := header.Open()
			if err != nil {
				t.Error(err)
				continue
			}
			cfg, _, err := image.DecodeConfig(file)
			file.Close()
			want := image.Pt(1536, 1024)
			if index == 1 {
				want = other.Bounds().Size()
			}
			if err != nil || image.Pt(cfg.Width, cfg.Height) != want {
				t.Errorf("reference %d: %+v, %v", index, cfg, err)
			}
		}
		WriteJSON(w, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(imageTestPNG(t, "1536x1024"))}}})
	})
	if _, err := a.EditImageMask(imageTestActor(), "test", refs, nil, "1536x1024"); err != nil {
		t.Fatal(err)
	}
	if refs[0] != portrait || FitToSize(other, "bad") != other {
		t.Fatal("caller references mutated")
	}
	landscape := image.NewNRGBA(image.Rect(0, 0, 1536, 1024))
	if FitToSize(landscape, "1536x1024") != landscape {
		t.Fatal("matching reference changed")
	}
}
func TestGenerateImageRetryQuotaAndUsage(t *testing.T) {
	b := imageTestPNG(t, "1024x1536")
	var calls atomic.Int64
	a := imageTestCore(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.URL.Path != "/images/generations" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("wrong generation request")
		}
		var req Object
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
		}
		if req["model"] != "override" || req["size"] != "1024x1536" || req["quality"] != "high" || req["n"] != float64(1) {
			t.Error(req)
		}
		if n == 1 {
			w.WriteHeader(503)
			io.WriteString(w, "private-key http://internal/secret")
			return
		}
		WriteJSON(w, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(b)}}})
	})
	if _, e := a.DB.Exec("INSERT INTO settings(key,value,updated_at) VALUES('ai_models',?,?)", JS(Object{"model_create": "override"}), Now()); e != nil {
		t.Fatal(e)
	}
	out, e := a.GenerateImage(imageTestActor(), "text", "1024x1536")
	if e != nil || !bytes.Equal(out, b) || calls.Load() != 2 {
		t.Fatal(e, calls.Load())
	}
	var feature, model string
	var uid, jid int64
	if e = a.DB.QueryRow("SELECT user_id,job_id,feature,model FROM ai_usage").Scan(&uid, &jid, &feature, &model); e != nil || uid != 1 || jid != 9 || feature != "model_create" || model != "override" {
		t.Fatal(e, uid, jid, feature, model)
	}
	if _, e = a.GenerateImage(imageTestActor(), "text", "1024x1536"); e == nil || calls.Load() != 2 {
		t.Fatal("quota did not stop HTTP", e)
	}
}
func TestImageResponseValidationAndReservationRelease(t *testing.T) {
	b := imageTestPNG(t)
	for _, tc := range []struct {
		name     string
		response Object
		status   int
	}{
		{"remote URL", Object{"data": []Object{{"url": "http://internal/secret"}}}, 200},
		{"multiple images", Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(b)}, {"b64_json": base64.StdEncoding.EncodeToString(b)}}}, 200},
		{"invalid base64", Object{"data": []Object{{"b64_json": "???"}}}, 200},
		{"invalid PNG", Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString([]byte("not png"))}}}, 200},
		{"truncated PNG", Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(b[:len(b)-8])}}}, 200},
		{"provider error", Object{"error": "private-key http://internal/secret"}, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			var success atomic.Bool
			a := imageTestCore(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if success.Load() {
					WriteJSON(w, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(b)}}})
				} else {
					WriteJSON(w, tc.status, tc.response)
				}
			})
			if _, e := a.GenerateImage(imageTestActor(), "text", "1024x1024"); e == nil || strings.Contains(e.Error(), "private-key") || strings.Contains(e.Error(), "internal") {
				t.Fatal(e)
			}
			var used int
			a.DB.QueryRow("SELECT count(*) FROM ai_usage").Scan(&used)
			if used != 0 || calls.Load() != 2 {
				t.Fatal("failed call charged or not retried", used, calls.Load())
			}
			success.Store(true)
			if _, e := a.GenerateImage(imageTestActor(), "text", "1024x1024"); e != nil {
				t.Fatal("reservation leaked", e)
			}
		})
	}
}
func TestImageExplicitSizeAndLegacyEdit(t *testing.T) {
	sizes := make(chan string, 3)
	counts := make(chan int, 3)
	a := imageTestCore(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Error(r.URL.Path)
		}
		mr, e := r.MultipartReader()
		if e != nil {
			t.Error(e)
			return
		}
		count := 0
		size := ""
		for {
			p, e := mr.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Error(e)
				return
			}
			data, _ := io.ReadAll(p)
			if p.FormName() == "size" {
				size = string(data)
			}
			if p.FormName() == "image" {
				count++
				if p.Header.Get("Content-Type") != "image/png" {
					t.Error("non-PNG reference")
				}
			}
			p.Close()
		}
		sizes <- size
		counts <- count
		WriteJSON(w, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(imageTestPNG(t, size))}}})
	})
	a.DB.Exec("UPDATE quotas SET month_images=5")
	refs := []image.Image{image.NewNRGBA(image.Rect(0, 0, 80, 40)), image.NewNRGBA(image.Rect(0, 0, 20, 30)), image.NewNRGBA(image.Rect(0, 0, 10, 10))}
	if _, e := a.EditImageSize(imageTestActor(), "text", refs, "1024x1536"); e != nil {
		t.Fatal(e)
	}
	if <-sizes != "1024x1536" || <-counts != 3 {
		t.Fatal("explicit size/order count lost")
	}
	if _, e := a.EditImage(imageTestActor(), "text", refs[:1]); e != nil {
		t.Fatal(e)
	}
	if <-sizes != "1536x1024" || <-counts != 1 {
		t.Fatal("legacy aspect behavior changed")
	}
	if _, e := a.EditImageSize(imageTestActor(), "text", refs, "999x999"); e == nil {
		t.Fatal("invalid size accepted")
	}
	if _, e := a.GenerateImage(imageTestActor(), "text", "999x999"); e == nil {
		t.Fatal("invalid generation size accepted")
	}
	if _, e := a.EditImage(imageTestActor(), "text", nil); e == nil {
		t.Fatal("empty references accepted")
	}
}
func TestImageCancellationAndTimeout(t *testing.T) {
	var calls atomic.Int64
	a := imageTestCore(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	a.Cfg.ImageTimeout = 25 * time.Millisecond
	if _, e := a.GenerateImage(imageTestActor(), "text", "1024x1024"); e == nil {
		t.Fatal("timeout accepted")
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	ctx, cancel := context.WithCancel(imageTestActor())
	cancel()
	if _, e := a.GenerateImage(ctx, "text", "1024x1024"); e == nil {
		t.Fatal("cancel accepted")
	}
	if calls.Load() != 2 {
		t.Fatal("canceled request sent")
	}
	var used int
	a.DB.QueryRow("SELECT count(*) FROM ai_usage").Scan(&used)
	if used != 0 {
		t.Fatal("timed out request charged")
	}
}

func TestImageReturnedSizeMismatch(t *testing.T) {
	wrong := imageTestPNG(t, "1536x1024")
	var calls atomic.Int64
	a := imageTestCore(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		WriteJSON(w, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(wrong)}}})
	})
	for _, edit := range []bool{false, true} {
		var err error
		if edit {
			_, err = a.EditImageMask(imageTestActor(), "mask", []image.Image{image.NewNRGBA(image.Rect(0, 0, 10, 10))}, image.NewAlpha(image.Rect(0, 0, 10, 10)), "1024x1536")
		} else {
			_, err = a.GenerateImage(imageTestActor(), "text", "1024x1536")
		}
		if err == nil || err.Error() != "图像服务返回的尺寸不对：请求 1024×1536，返回 1536×1024" {
			t.Fatal(err)
		}
	}
	if calls.Load() != 4 {
		t.Fatal(calls.Load())
	}
	var usage int
	_ = a.DB.QueryRow("SELECT count(*) FROM ai_usage").Scan(&usage)
	if usage != 0 {
		t.Fatal("failed image consumed quota")
	}
}

func TestEditImageMaskMultipartOrderAndValidation(t *testing.T) {
	a := imageTestCore(t, func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		names := []string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				return
			}
			name := part.FormName()
			if name == "image" || name == "mask" {
				names = append(names, name)
				if part.Header.Get("Content-Type") != "image/png" {
					t.Error("wrong PNG content type")
				}
				if _, err = png.Decode(part); err != nil {
					t.Error(err)
				}
			}
			part.Close()
		}
		if strings.Join(names, ",") != "image,image,mask" {
			t.Error(names)
		}
		WriteJSON(w, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(imageTestPNG(t))}}})
	})
	refs := []image.Image{image.NewNRGBA(image.Rect(0, 0, 10, 20)), image.NewNRGBA(image.Rect(0, 0, 8, 8))}
	if _, err := a.EditImageMask(imageTestActor(), "edit", refs, image.NewAlpha(image.Rect(0, 0, 9, 20)), "1024x1024"); err == nil {
		t.Fatal("bad mask accepted")
	}
	if _, err := a.EditImageMask(imageTestActor(), "edit", refs, image.NewAlpha(image.Rect(0, 0, 10, 20)), "1024x1024"); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateImageStyleReferences(t *testing.T) {
	app := imageTestCore(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/images/edits" {
			t.Error("style reference was discarded", request.URL.Path)
		}
		if err := request.ParseMultipartForm(8 << 20); err != nil {
			t.Error(err)
			return
		}
		defer request.MultipartForm.RemoveAll()
		if len(request.MultipartForm.File["image"]) != 1 || request.FormValue("size") != "1024x1024" {
			t.Error("missing style reference")
		}
		WriteJSON(writer, 200, Object{"data": []Object{{"b64_json": base64.StdEncoding.EncodeToString(imageTestPNG(t))}}})
	})
	if _, err := app.GenerateImage(imageTestActor(), "background", "1024x1024", image.NewNRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
}
