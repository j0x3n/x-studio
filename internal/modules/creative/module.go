// Package creative 是 AI 出图模块：生图模板、每个上架版本的一套主图和详情页、
// 像素锁定合成、模特图、三层复核和分流、改字、导出。
package creative

import (
	"sync"
	"xstudio/internal/core"
)

type Module struct {
	*core.Core
	returnOnce sync.Once
}

func New(c *core.Core) *Module { return &Module{Core: c} }
func (*Module) Name() string   { return "creative" }
