// Package market 是数据与运营模块：采集数据接收、报表导入、关键词和竞品、
// 诊断规则和运营建议、个人采集令牌。契约见 接口约定-数据与运营.md。
package market

import "xstudio/internal/core"

type Module struct{ *core.Core }

func New(c *core.Core) *Module                   { return &Module{c} }
func (*Module) Name() string                     { return "market" }
func (*Module) Migrations() []core.Migration     { return nil }
func (*Module) Routes(*core.Router)              {}
func (*Module) Jobs() map[string]core.JobHandler { return nil }
