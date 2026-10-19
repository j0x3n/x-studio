package accounts

import (
	"database/sql"
	"encoding/json"
	_ "modernc.org/sqlite"
	"testing"
	"xstudio/internal/core"
)

func TestModelPermissionMigrationPreservesRoleCustomization(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("CREATE TABLE roles(key TEXT PRIMARY KEY,builtin INTEGER,perms TEXT)"); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"operator", "designer", "photographer", "collaborator", "custom"} {
		if _, e = db.Exec("INSERT INTO roles VALUES(?,1,?)", key, core.JS([]string{"existing.custom"})); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		tx, e := db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		if e = modelPermissionsMigration(tx); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	for _, key := range []string{"operator", "designer", "photographer", "collaborator", "custom"} {
		var raw string
		if e = db.QueryRow("SELECT perms FROM roles WHERE key=?", key).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		var perms []string
		if e = json.Unmarshal([]byte(raw), &perms); e != nil {
			t.Fatal(e)
		}
		counts := map[string]int{}
		for _, p := range perms {
			counts[p]++
		}
		if counts["existing.custom"] != 1 {
			t.Fatal("customization lost", key, perms)
		}
		view, manage := 0, 0
		if key == "operator" || key == "designer" {
			view, manage = 1, 1
		}
		if key == "photographer" {
			view = 1
		}
		if counts[core.ModelView] != view || counts[core.ModelManage] != manage {
			t.Fatal(key, perms)
		}
	}
	if _, e = db.Exec("UPDATE roles SET builtin=0,perms='[]' WHERE key='operator'"); e != nil {
		t.Fatal(e)
	}
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if e = modelPermissionsMigration(tx); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	var raw string
	db.QueryRow("SELECT perms FROM roles WHERE key='operator'").Scan(&raw)
	if raw != "[]" {
		t.Fatal("modified non-builtin role", raw)
	}
}
