// Package creative 是 AI 出图模块：生图模板、每个上架版本的一套主图和详情页、
// 像素锁定合成、模特图、三层复核和分流、改字、导出。契约见 接口约定-AI出图.md。
package creative

import "xstudio/internal/core"

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "creative" }
func (*Module) Migrations() []core.Migration     { return nil }
func (*Module) Routes(*core.Router)              {}
func (*Module) Jobs() map[string]core.JobHandler { return nil }
