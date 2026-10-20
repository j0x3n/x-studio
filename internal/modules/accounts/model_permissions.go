package accounts

import (
	"database/sql"
	"xstudio/internal/core"
)

func modelPermissionsMigration(tx *sql.Tx) error {
	for _, key := range []string{"operator", "designer"} {
		if err := appendBuiltinPerms(tx, key, core.ModelView, core.ModelManage); err != nil {
			return err
		}
	}
	return appendBuiltinPerms(tx, "photographer", core.ModelView)
}

// AI 工作流改造：AI 出图、数据与运营的权限点。运营全给除模板外的；美工管出图和模板；摄影不给。
func creativePermissionsMigration(tx *sql.Tx) error {
	if err := appendBuiltinPerms(tx, "operator", core.CreativeRun, core.CreativeReview, core.MarketView, core.MarketImport, core.OpsAdvice); err != nil {
		return err
	}
	return appendBuiltinPerms(tx, "designer", core.CreativeRun, core.CreativeReview, core.CreativeTemplate)
}
