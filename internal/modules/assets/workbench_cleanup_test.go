package assets

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkbenchCleanupExpiredSavedAndOrphanFiles(t *testing.T) {
	a := algorithmModule(t)
	if _, err := a.DB.Exec("CREATE TABLE jobs(target_type TEXT,target_id INTEGER,status TEXT)"); err != nil {
		t.Fatal(err)
	}
	root, err := a.workbenchRoot(1)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	root.MkdirAll("inputs", 0700)
	root.MkdirAll("results", 0700)
	root.MkdirAll(".thumbs", 0700)
	for _, file := range []string{"inputs/old.png", "results/saved.png", "results/live.png", "results/orphan.png", ".thumbs/old.jpg"} {
		if err = root.WriteFile(file, []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	saved := filepath.Join(a.Cfg.DataDir, "saved-library.png")
	if err = os.WriteFile(saved, []byte("asset copy"), 0600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	for _, file := range []string{"results/orphan.png", ".thumbs/old.jpg"} {
		if err = root.Chtimes(file, at.Add(-2*time.Hour), at.Add(-2*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	old := at.Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)
	fresh := at.Format(time.RFC3339Nano)
	for _, v := range []struct {
		file, created string
		saved         any
	}{{"inputs/old.png", old, nil}, {"results/saved.png", old, 123}, {"results/live.png", fresh, nil}, {"inputs/missing.png", old, nil}} {
		if _, err = a.DB.Exec("INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,created_at,saved_asset_id) VALUES(1,'result',?,'x.png',1,1,?,?)", v.file, v.created, v.saved); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.cleanupWorkbench(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	var n int
	a.DB.QueryRow("SELECT count(*) FROM workbench_items").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	for _, file := range []string{"inputs/old.png", "results/saved.png", "results/orphan.png", ".thumbs/old.jpg"} {
		if _, err = root.Stat(file); !os.IsNotExist(err) {
			t.Fatal(file, err)
		}
	}
	if _, err = root.Stat("results/live.png"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(saved); err != nil {
		t.Fatal("清理误删素材库", err)
	}
	// Protect files staged by an active job, then collect them on the next pass.
	release := a.holdWorkbenchFile(1, "results/staged.png")
	defer release()
	root.WriteFile("results/staged.png", []byte("staged"), 0600)
	if err = root.Chtimes("results/staged.png", at.Add(-2*time.Hour), at.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	a.DB.Exec("INSERT INTO jobs VALUES('user',1,'running')")
	root.WriteFile("inputs/expired-while-running.png", []byte("expired"), 0600)
	if _, err = a.DB.Exec("INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,created_at) VALUES(1,'input','inputs/expired-while-running.png','old.png',1,1,?)", old); err != nil {
		t.Fatal(err)
	}
	if err = a.cleanupWorkbench(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if _, err = root.Stat("results/staged.png"); err != nil {
		t.Fatal("清理了运行中结果", err)
	}
	if _, err = root.Stat("inputs/expired-while-running.png"); !os.IsNotExist(err) {
		t.Fatal("运行任务不应延后过期清理", err)
	}
	release()
	a.DB.Exec("UPDATE jobs SET status='done'")
	if err = a.cleanupWorkbench(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if _, err = root.Stat("results/staged.png"); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	// Expired database rows are still removed if an entire user directory vanished.
	if _, err = a.DB.Exec("INSERT INTO workbench_items(user_id,kind,file,orig_name,width,height,created_at) VALUES(2,'input','inputs/gone.png','gone.png',1,1,?)", old); err != nil {
		t.Fatal(err)
	}
	if err = a.cleanupWorkbench(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	a.DB.QueryRow("SELECT count(*) FROM workbench_items WHERE user_id=2").Scan(&n)
	if n != 0 {
		t.Fatal("未清理无目录的过期记录")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = a.cleanupWorkbench(ctx, at); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestWorkbenchRootRejectsTraversalAndSymlinkEscape(t *testing.T) {
	a := algorithmModule(t)
	root, err := a.workbenchRoot(1)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, path := range []string{"../outside.png", "/absolute.png", "C:/absolute.png", "inputs\\outside.png"} {
		if _, err = workbenchRead(root, path); err == nil {
			t.Fatal("允许越界", path)
		}
	}
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.png")
	if err = os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	// Windows may not grant symlink creation to the test account.
	if err = os.Symlink(outside, filepath.Join(a.Cfg.DataDir, "workbench", "1", "escape")); err != nil {
		t.Logf("系统未授予符号链接创建权限：%v", err)
		return
	}
	if _, err = workbenchRead(root, "escape/secret.png"); err == nil {
		t.Fatal("允许符号链接逃逸")
	}
	if err = root.WriteFile("escape/changed.png", []byte("bad"), 0600); err == nil {
		t.Fatal("允许越界写入")
	}
}
