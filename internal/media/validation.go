package media

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
)

var CodePattern = regexp.MustCompile(`^[A-Z][1-9][0-9]?(?:_[\p{L}\p{N}_-]+)?$`)

// AISuffix 标出 AI 补的图，和同位置的实拍分开；实拍照片不能用这个后缀。
const AISuffix = "_AI"

// FillBase 返回 AI 补图编号对应的位置编号：A1_AI → A1。只认 A1、E1、E2、E3。
func FillBase(code string) (string, bool) {
	base, ok := strings.CutSuffix(code, AISuffix)
	if !ok {
		return "", false
	}
	switch base {
	case "A1", "E1", "E2", "E3":
		return base, true
	}
	return "", false
}

// AIMade 表示画面由 AI 生成或改过（高清化不算），这类图不能放进实拍证据位。
func AIMade(p core.Photo) bool {
	s := p.EffectiveSource()
	if p.AIGenerated || s == "ai_generated" || s == "ai_edited" {
		return true
	}
	if s == "ai_enhanced" || p.Source == "ai_enhanced" || (p.AIEnhanced && p.EnhancedFile == "") {
		return p.EnhanceCheck["verdict"] != "pass"
	}
	return false
}

func ValidatePhoto(p core.Photo) error {
	if _, ok := FillBase(p.Code); p.AIGenerated && p.Code != "" && !ok {
		return errors.New("AI 生成照片只能使用 A1_AI、E1_AI、E2_AI、E3_AI 编号")
	}
	if !p.AIGenerated && strings.HasSuffix(p.Code, AISuffix) {
		return errors.New("_AI 是 AI 补图保留后缀，请换一个拍摄编号")
	}
	if p.Code != "" && (!CodePattern.MatchString(p.Code) || utf8.RuneCountInString(p.Code) > 60) {
		return errors.New("拍摄编号格式不正确")
	}
	if strings.HasSuffix(p.Code, "_白底") || strings.HasSuffix(p.Code, "_透明") {
		return errors.New("白底、透明是输出文件保留后缀，请换一个拍摄编号")
	}
	if p.Rotate != 0 && p.Rotate != 90 && p.Rotate != 180 && p.Rotate != 270 {
		return errors.New("旋转角度只能是 0、90、180、270")
	}
	if p.Crop != nil {
		if len(p.Crop) != 4 {
			return errors.New("裁剪必须是 [x,y,w,h]")
		}
		c := p.Crop
		if c[0] < 0 || c[1] < 0 || c[2] <= 0 || c[3] <= 0 || c[0]+c[2] > 1.00000001 || c[1]+c[3] > 1.00000001 {
			return errors.New("裁剪范围必须在图片比例 0–1 之间")
		}
	}
	return nil
}
func ValidateInfo(o core.Object) error {
	for _, k := range []string{"auto_upscale", "auto_fill"} {
		if v, exists := o[k]; exists {
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("info.%s 必须是布尔值", k)
			}
		}
	}
	return nil
}
