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

func ValidatePhoto(p core.Photo) error {
	if p.AIGenerated && p.Code != "" && p.Code != "A1" && p.Code != "E1" && p.Code != "E2" && p.Code != "E3" && p.Code != "D4" {
		return errors.New("AI 生成照片只能使用 A1、E1、E2、E3、D4 编号")
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
