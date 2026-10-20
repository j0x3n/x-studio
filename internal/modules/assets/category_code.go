package assets

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
	"xstudio/internal/media"
)

// Category codes allow zero as well as 1–9 in the numeric part. Keep the
// existing media checks for geometry and AI-generated photo restrictions.
var categoryPhotoCode = regexp.MustCompile(`^[A-Z][0-9]{1,2}(?:_[\p{L}\p{N}_-]+)?$`)

func validatePhoto(p core.Photo) error {
	if p.AIGenerated {
		return media.ValidatePhoto(p)
	}
	if p.Code != "" && (!categoryPhotoCode.MatchString(p.Code) || utf8.RuneCountInString(p.Code) > 60) {
		return errors.New("拍摄编号格式不正确")
	}
	if strings.HasSuffix(p.Code, "_白底") || strings.HasSuffix(p.Code, "_透明") {
		return errors.New("白底、透明是输出文件保留后缀，请换一个拍摄编号")
	}
	if strings.HasSuffix(p.Code, media.AISuffix) {
		return errors.New("_AI 是 AI 补图保留后缀，请换一个拍摄编号")
	}
	p.Code = ""
	return media.ValidatePhoto(p)
}
