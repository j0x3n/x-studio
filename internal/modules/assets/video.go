package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xstudio/internal/core"
)

const videoProbeTimeout = 30 * time.Second

var maxVideoBytes int64 = 500 << 20
var ffmpegBin string
var ffprobeBin string

func readPrefix(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	got, err := io.ReadFull(r, buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return buf[:got], nil
	}
	return buf[:got], err
}

func videoBlocked(p Asset, what string) error {
	if p.Media != "video" {
		return nil
	}
	return fmt.Errorf("视频不能用来%s", what)
}

func toolPath(override, envKey, name string) (string, error) {
	path := strings.TrimSpace(override)
	if path == "" {
		path = strings.TrimSpace(os.Getenv(envKey))
	}
	if path == "" {
		found, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("没有找到 %s", name)
		}
		return found, nil
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", fmt.Errorf("没有找到 %s", name)
	}
	return path, nil
}

func runTool(bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), videoProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("处理超时")
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("没有找到 %s", filepath.Base(bin))
		}
		return stderr.Bytes(), err
	}
	return stdout.Bytes(), nil
}

type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexString(strconv.FormatFloat(n, 'f', -1, 64))
	return nil
}

type videoProbe struct {
	Streams []struct {
		Width    int        `json:"width"`
		Height   int        `json:"height"`
		Duration flexString `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration flexString `json:"duration"`
	} `json:"format"`
}

func parseDurationMS(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return int(math.Round(f * 1000)), true
}

func grabPoster(bin, video, poster, ss string) error {
	_, err := runTool(bin, "-hide_banner", "-loglevel", "error", "-y", "-ss", ss, "-i", video, "-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", poster)
	if err != nil {
		if err.Error() == "处理超时" || strings.HasPrefix(err.Error(), "没有找到 ") {
			return err
		}
		return fmt.Errorf("截取封面失败")
	}
	f, err := os.Open(poster)
	if err != nil {
		return fmt.Errorf("截取封面失败")
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil || format != "jpeg" || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 1280 {
		return fmt.Errorf("截取封面失败")
	}
	return nil
}

func inspectVideo(path string) (width, height, duration int, poster string, note string) {
	ffmpeg, err := toolPath(ffmpegBin, "FFMPEG", "ffmpeg")
	ffprobe, err2 := toolPath(ffprobeBin, "FFPROBE", "ffprobe")
	if err != nil {
		return 0, 0, 0, "", err.Error()
	}
	if err2 != nil {
		return 0, 0, 0, "", err2.Error()
	}
	raw, err := runTool(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,duration:format=duration", "-of", "json", path)
	if err != nil {
		if err.Error() == "处理超时" || strings.HasPrefix(err.Error(), "没有找到 ") {
			return 0, 0, 0, "", err.Error()
		}
		return 0, 0, 0, "", "读取视频信息失败"
	}
	var probe videoProbe
	if json.Unmarshal(raw, &probe) != nil || len(probe.Streams) == 0 || probe.Streams[0].Width < 1 || probe.Streams[0].Height < 1 {
		return 0, 0, 0, "", "读取视频信息失败"
	}
	width, height = probe.Streams[0].Width, probe.Streams[0].Height
	if ms, ok := parseDurationMS(string(probe.Streams[0].Duration)); ok {
		duration = ms
	} else if ms, ok = parseDurationMS(string(probe.Format.Duration)); ok {
		duration = ms
	}
	posterFile, err := os.CreateTemp(filepath.Dir(path), ".poster-*.jpg")
	if err != nil {
		return 0, 0, 0, "", "截取封面失败"
	}
	poster = posterFile.Name()
	posterFile.Close()
	ss := "0"
	if duration >= 1000 {
		ss = "1"
	}
	if err = grabPoster(ffmpeg, path, poster, ss); err != nil && ss != "0" {
		err = grabPoster(ffmpeg, path, poster, "0")
	}
	if err != nil {
		os.Remove(poster)
		if err.Error() == "处理超时" || strings.HasPrefix(err.Error(), "没有找到 ") {
			return 0, 0, 0, "", err.Error()
		}
		return 0, 0, 0, "", "截取封面失败"
	}
	return width, height, duration, poster, ""
}

type savedVideo struct {
	tmp, posterTmp     string
	file, posterFile   string
	width, height, dur int
	metrics            Object
}

func (a *Module) saveIncomingVideo(productID int64, head []byte, body io.Reader, ext string) (savedVideo, int, string) {
	dir := filepath.Join(a.Dir(productID), "原图", "视频")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return savedVideo{}, 500, "无法创建视频目录"
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return savedVideo{}, 500, "保存上传视频失败"
	}
	tmpName := tmp.Name()
	remove := true
	defer func() {
		if remove {
			os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(head); err != nil {
		tmp.Close()
		return savedVideo{}, 500, "保存上传视频失败"
	}
	limit := maxVideoBytes - int64(len(head))
	if limit < 0 {
		tmp.Close()
		return savedVideo{}, 413, "视频不能超过 500 MB"
	}
	n, err := io.Copy(tmp, io.LimitReader(body, limit+1))
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		return savedVideo{}, 500, "保存上传视频失败"
	}
	if int64(len(head))+n > maxVideoBytes {
		return savedVideo{}, 413, "视频不能超过 500 MB"
	}
	width, height, dur, poster, note := inspectVideo(tmpName)
	out := savedVideo{tmp: tmpName, file: "原图/视频/" + rand.Text() + "." + ext, metrics: Object{}}
	if note != "" {
		os.Remove(poster)
		out.metrics = Object{"video_note": "没有生成封面：" + note}
		remove = false
		return out, 0, ""
	}
	out.posterTmp = poster
	out.posterFile = "原图/视频/" + rand.Text() + ".jpg"
	out.width, out.height, out.dur = width, height, dur
	remove = false
	return out, 0, ""
}

func (a *Module) poster(w http.ResponseWriter, r *http.Request) {
	p, err := a.asset(idParam(r, "pid"))
	if err != nil || !a.CanProduct(currentUser(r), p.ProductID) {
		fail(w, 404, "素材不存在")
		return
	}
	if p.PosterFile == "" || !validAssetFile(p.PosterFile) {
		fail(w, 404, "没有封面")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	core.ServeRoot(w, r, a.Dir(p.ProductID), p.PosterFile)
}
