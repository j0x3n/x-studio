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
