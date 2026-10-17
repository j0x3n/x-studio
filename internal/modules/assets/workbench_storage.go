package assets

import (
	"crypto/rand"
	"fmt"
	"os"
	"path"
	"xstudio/internal/imaging"
)

// holdWorkbenchFile is for unique, randomly named files. Register BEFORE
// creating the file; release only AFTER closing and publishing or removing it.
// Cleanup can therefore skip even an hours-old file without taking Core.Mu in
// its writer. The one-hour orphan grace also covers interrupted/crashed writes.
func (a *Module) holdWorkbenchFile(uid int64, file string) func() {
	key := fmt.Sprintf("%d/%s", uid, file)
	a.workbenchFiles.Store(key, true)
	return func() { a.workbenchFiles.Delete(key) }
}
func workbenchThumbPath(file string, width int) string {
	return fmt.Sprintf(".thumbs/%d/%s.jpg", width, file)
}
func removeWorkbenchThumbs(root *os.Root, file string) error {
	if !validAssetFile(file) {
		return fmt.Errorf("素材文件路径不合法")
	}
	for _, width := range []int{240, 480, 960} {
		if err := root.Remove(workbenchThumbPath(file, width)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// publishWorkbenchThumb coordinates only the final cache rename with item
// deletion. Image decoding, encoding and temporary-file writes take no mutex.
func (a *Module) publishWorkbenchThumb(root *os.Root, v WorkbenchItem, width int, b []byte) error {
	cache := workbenchThumbPath(v.File, width)
	if err := root.MkdirAll(path.Dir(cache), 0700); err != nil {
		return err
	}
	tmp := cache + "." + rand.Text() + ".tmp"
	release := a.holdWorkbenchFile(v.UserID, tmp)
	defer release()
	defer root.Remove(tmp)
	if err := root.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	a.workbenchFileMu.Lock()
	defer a.workbenchFileMu.Unlock()
	fresh, err := a.workbenchItem(v.UserID, v.ID)
	if err != nil || fresh.File != v.File {
		return os.ErrNotExist
	}
	// Another request may have finished the same immutable thumbnail first.
	if _, err = root.Stat(cache); err == nil {
		return nil
	}
	return root.Rename(tmp, cache)
}
func (a *Module) workbenchThumbnail(root *os.Root, v WorkbenchItem, width int) ([]byte, error) {
	if !validAssetFile(v.File) {
		return nil, fmt.Errorf("素材文件路径不合法")
	}
	cache := workbenchThumbPath(v.File, width)
	if b, err := workbenchRead(root, cache); err == nil {
		return b, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	im, ex, err := workbenchLoad(root, v.File)
	if err != nil {
		return nil, err
	}
	oriented := imaging.Geometry(im, ex.Orientation, 0, nil)
	target := min(width, oriented.Rect.Dx())
	long := max(1, target*max(oriented.Rect.Dx(), oriented.Rect.Dy())/oriented.Rect.Dx())
	resized, err := imaging.Resample(oriented, long)
	if err != nil {
		return nil, err
	}
	b, err := encodeWorkbench(imaging.Flatten(resized), "jpg")
	if err != nil {
		return nil, err
	}
	if err = a.publishWorkbenchThumb(root, v, width, b); err != nil {
		return nil, err
	}
	return b, nil
}
