package pipeline

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"xstudio/internal/core"
)

type Browser struct {
	path        string
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	allocCancel context.CancelFunc
}

func ChromePath(configured string) string {
	if configured != "" {
		return configured
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "msedge"} {
		if p, e := exec.LookPath(name); e == nil {
			return p
		}
	}
	for _, p := range []string{`C:\Program Files\Google\Chrome\Application\chrome.exe`, `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`, filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"), `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`} {
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
func NewBrowser(p string) *Browser { return &Browser{path: ChromePath(p)} }
func (b *Browser) start() (context.Context, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx != nil && b.ctx.Err() != nil {
		b.cancel()
		b.allocCancel()
		b.ctx = nil
	}
	if b.ctx != nil {
		return b.ctx, nil
	}
	if b.path == "" {
		return nil, fmt.Errorf("未找到 Chrome，请安装 Chrome 或设置 CHROME_PATH")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(b.path), chromedp.Flag("hide-scrollbars", true), chromedp.Flag("disable-dev-shm-usage", true), chromedp.Flag("disable-gpu", true))
	// The app container runs as an unprivileged user. Some Docker kernels disable
	// Chromium user namespaces; this flag is opt-in for that deployment only.
	if os.Getenv("CHROME_NO_SANDBOX") == "1" {
		opts = append(opts, chromedp.NoSandbox)
	}
	alloc, ac := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(alloc)
	// The first Run owns the browser lifetime. A temporary timeout context here
	// would kill the shared browser immediately after successful initialization.
	timer := time.AfterFunc(30*time.Second, cancel)
	if e := chromedp.Run(ctx); e != nil {
		timer.Stop()
		cancel()
		ac()
		return nil, fmt.Errorf("Chrome 启动失败：%w", e)
	}
	timer.Stop()
	b.ctx, b.cancel, b.allocCancel = ctx, cancel, ac
	return ctx, nil
}
func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
		b.allocCancel()
		b.ctx = nil
	}
}
func (b *Browser) Shot(parent context.Context, address string, w, h int64, scale float64, main bool) ([]byte, error) {
	root, e := b.start()
	if e != nil {
		return nil, e
	}
	tab, cancel := chromedp.NewContext(root)
	defer cancel()
	ctx, tc := context.WithTimeout(tab, 60*time.Second)
	defer tc()
	stop := context.AfterFunc(parent, tc)
	defer stop()
	var buf []byte
	var hasAI bool
	ready := `(window.PRODUCT || window.MAIN) && (document.querySelector('.cv') || document.querySelector('#page'))`
	if main {
		ready = `window.MAIN && document.querySelector('.cv')`
	}
	e = chromedp.Run(ctx, chromedp.EmulateViewport(w, h, chromedp.EmulateScale(scale)), chromedp.Navigate(address), chromedp.Poll(ready, nil), chromedp.ActionFunc(func(ctx context.Context) error {
		_, ex, err := runtime.Evaluate(`(async()=>{await document.fonts.ready; await Promise.all(Array.from(document.images).map(img=>img.complete?Promise.resolve():new Promise(resolve=>{img.addEventListener('load',resolve,{once:true});img.addEventListener('error',resolve,{once:true})}))); if(typeof prune==='function')prune(); await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))); return Array.from(document.images).every(img=>img.complete);})()`).WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
		if err != nil {
			return err
		}
		if ex != nil {
			return fmt.Errorf("字体或图片等待失败")
		}
		return nil
	}), chromedp.Evaluate(`Array.from(document.querySelectorAll(".ai-tag")).some(el=>el.getClientRects().length>0)`, &hasAI), chromedp.CaptureScreenshot(&buf))
	if e != nil {
		return nil, fmt.Errorf("Chrome 截图失败：%w", e)
	}
	if hasAI {
		return core.ImplicitLabel(buf)
	}
	return buf, nil
}

func isGreen(r, g, b uint32) bool { // ImageMagick -fuzz 2% in RGB distance.
	return float64(r>>8)*float64(r>>8)+math.Pow(float64(g>>8)-255, 2)+float64(b>>8)*float64(b>>8) <= math.Pow(.02*255*math.Sqrt(3), 2)
}
func TrimGreen(src image.Image) image.Image {
	b := src.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := src.At(x, y).RGBA()
			if !isGreen(r, g, bl) {
				minX = min(minX, x)
				minY = min(minY, y)
				maxX = max(maxX, x+1)
				maxY = max(maxY, y+1)
			}
		}
	}
	if minX >= maxX || minY >= maxY {
		return nil
	}
	rect := image.Rect(minX, minY, maxX, maxY)
	dst := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			dst.Set(x, y, src.At(x+rect.Min.X, y+rect.Min.Y))
		}
	}
	return dst
}
func DetailSlices(src image.Image) []image.Image {
	trim := TrimGreen(src)
	if trim == nil {
		return nil
	}
	b := trim.Bounds()
	parts := (b.Dy() + 1499) / 1500
	ph := (b.Dy() + parts - 1) / parts
	out := []image.Image{}
	for y := 0; y < b.Dy(); y += ph {
		rect := image.Rect(0, y, b.Dx(), min(b.Dy(), y+ph))
		d := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
		for dy := 0; dy < rect.Dy(); dy++ {
			for x := 0; x < rect.Dx(); x++ {
				d.Set(x, dy, trim.At(x, y+dy))
			}
		}
		out = append(out, d)
	}
	return out
}
func saveMain(filename string, im image.Image) error {
	for _, q := range []int{92, 82, 72, 62, 52, 42, 32, 22, 12, 5} {
		var b bytes.Buffer
		if e := jpeg.Encode(&b, im, &jpeg.Options{Quality: q}); e != nil {
			return e
		}
		if b.Len() <= 900000 {
			return os.WriteFile(filename, b.Bytes(), 0600)
		}
	}
	return fmt.Errorf("主图压缩后仍超过 900 KB")
}
func (a *Module) render(ctx context.Context, p Product) error {
	p.Main = cleanMain(p.Main, filepath.Join(a.Dir(p.ID), "照片"))
	if e := a.annotateForExport(p); e != nil {
		return e
	}
	if e := validateCopy(p.Content, p.Main, p.Listing); e != nil {
		return e
	}
	// The render page reads main from SQLite. Persist exactly the same filtered
	// and numbered list that we are about to screenshot.
	if _, e := a.DB.Exec("UPDATE listings SET main=?,content=?,render_ai_label=? WHERE id=?", js(p.Main), js(p.Content), p.AILabel, p.ListingID); e != nil {
		return fmt.Errorf("保存清理后的主图失败：%w", e)
	}
	imgs, _ := p.Main["imgs"].([]any)
	if len(imgs) == 0 {
		return fmt.Errorf("没有可用的主图照片，至少需要一张上耳图或场景图")
	}
	for _, name := range []string{"主图模板", "详情页模板"} {
		if _, e := os.Stat(filepath.Join(a.Cfg.TemplateDir, name, "模板.html")); e != nil {
			return fmt.Errorf("缺少%s，请先运行同步脚本", name)
		}
	}
	if e := os.MkdirAll(a.ListingDir(p.ListingID), 0700); e != nil {
		return e
	}
	tmp, e := os.MkdirTemp(a.ListingDir(p.ListingID), ".render-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	for _, sub := range []string{"主图/3x4", "主图/1x1", "详情页"} {
		if e = os.MkdirAll(filepath.Join(tmp, filepath.FromSlash(sub)), 0700); e != nil {
			return e
		}
	}
	base := a.getBaseURL()
	numbers := []int{}
	for _, v := range imgs {
		m := v.(Object)
		numbers = append(numbers, int(m["n"].(float64)))
	}
	sort.Ints(numbers)
	for _, n := range numbers {
		for _, r := range []struct {
			ratio, dir          string
			height              int64
			scale               float64
			widthOut, heightOut int
		}{{"34", "3x4", 1000, 1.92, 1440, 1920}, {"11", "1x1", 750, 1.6, 1200, 1200}} {
			q := url.Values{"p": {fmt.Sprint(p.ListingID)}, "n": {fmt.Sprint(n)}, "r": {r.ratio}, "_t": {a.renderToken()}}
			buf, e := a.browser.Shot(ctx, base+"/render/主图模板/模板.html?"+q.Encode(), 750, r.height, r.scale, true)
			if e != nil {
				return e
			}
			im, e := png.Decode(bytes.NewReader(buf))
			if e != nil {
				return e
			}
			if im.Bounds().Dx() != r.widthOut || im.Bounds().Dy() != r.heightOut {
				return fmt.Errorf("主图截图尺寸不正确：%v", im.Bounds())
			}
			filename := filepath.Join(tmp, "主图", r.dir, fmt.Sprintf("%02d.jpg", n))
			if e = saveMain(filename, im); e != nil {
				return e
			}
			if p.AILabel && bytes.Contains(buf, []byte("X Studio")) {
				if e = core.LabelFile(filename); e != nil {
					return e
				}
			}
		}
	}
	for s := 1; s <= 9; s++ {
		q := url.Values{"p": {fmt.Sprint(p.ListingID)}, "s": {fmt.Sprint(s)}, "_t": {a.renderToken()}}
		buf, e := a.browser.Shot(ctx, base+"/render/详情页模板/模板.html?"+q.Encode(), 750, 6000, 1, false)
		if e != nil {
			return e
		}
		im, e := png.Decode(bytes.NewReader(buf))
		if e != nil {
			return e
		}
		parts := DetailSlices(im)
		for i, part := range parts {
			name := fmt.Sprintf("%02d.jpg", s)
			if len(parts) > 1 {
				name = fmt.Sprintf("%02d-%d.jpg", s, i+1)
			}
			filename := filepath.Join(tmp, "详情页", name)
			if e = saveJPEG(filename, part, 88); e != nil {
				return e
			}
			if p.AILabel && bytes.Contains(buf, []byte("X Studio")) {
				if e = core.LabelFile(filename); e != nil {
					return e
				}
			}
		}
	}
	// 淘宝白底图不能带任何文字，AI 生成的白底图没法打标识，所以只导出实拍的白底图
	aiCodes := map[string]bool{}
	if phs, pe := a.photos(p.ID); pe == nil {
		for _, ph := range phs {
			if ph.EffectiveSource() == "ai_generated" || ph.EffectiveSource() == "ai_edited" {
				aiCodes[ph.Code] = true
			}
		}
	}
	for _, c := range []string{"A1", "A2"} {
		if aiCodes[c] {
			continue
		}
		b, e := os.ReadFile(filepath.Join(a.Dir(p.ID), "照片", c+"_白底.jpg"))
		if e == nil {
			if e = os.WriteFile(filepath.Join(tmp, "白底.jpg"), b, 0600); e != nil {
				return e
			}
			break
		}
	}
	text := listingText(p.Listing)
	if e = os.WriteFile(filepath.Join(tmp, "标题和SKU.txt"), []byte(text), 0600); e != nil {
		return e
	}
	zipTmp, e := os.CreateTemp(a.ListingDir(p.ListingID), ".export-*.zip")
	if e != nil {
		return e
	}
	zipName := zipTmp.Name()
	_ = zipTmp.Close()
	defer os.Remove(zipName)
	if e = packageZip(tmp, zipName); e != nil {
		return e
	}
	if e = replaceDir(tmp, filepath.Join(a.ListingDir(p.ListingID), "导出")); e != nil {
		return e
	}
	_ = os.Remove(filepath.Join(a.ListingDir(p.ListingID), "导出.zip"))
	if e = os.Rename(zipName, filepath.Join(a.ListingDir(p.ListingID), "导出.zip")); e != nil {
		return e
	}
	_, e = a.DB.Exec("UPDATE listings SET ai_label=? WHERE id=?", p.AILabel, p.ListingID)
	return e
}
func listingText(l Object) string {
	var b strings.Builder
	fmt.Fprintf(&b, "标题：%v\n", valueOrEmpty(l["title"]))
	if arr, ok := l["title_alts"].([]any); ok {
		for _, v := range arr {
			fmt.Fprintf(&b, "备选标题：%v\n", v)
		}
	}
	for _, k := range []string{"attrs", "sku", "compliance"} {
		fmt.Fprintf(&b, "\n%s：\n%s\n", k, js(l[k]))
	}
	return b.String()
}
func valueOrEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}
func packageZip(dir, dst string) error {
	f, e := os.Create(dst)
	if e != nil {
		return e
	}
	z := zip.NewWriter(f)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("不允许打包非普通文件")
		}
		rel, e := filepath.Rel(dir, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasPrefix(rel, "主图/3x4/"):
			rel = "主图3x4/" + strings.TrimPrefix(rel, "主图/3x4/")
		case strings.HasPrefix(rel, "主图/1x1/"):
			rel = "主图1x1/" + strings.TrimPrefix(rel, "主图/1x1/")
		}
		entry, e := z.Create(rel)
		if e != nil {
			return e
		}
		src, e := os.Open(p)
		if e != nil {
			return e
		}
		_, e = io.Copy(entry, src)
		ce := src.Close()
		if e != nil {
			return e
		}
		return ce
	})
	ze := z.Close()
	fe := f.Close()
	if err != nil {
		return err
	}
	if ze != nil {
		return ze
	}
	return fe
}
