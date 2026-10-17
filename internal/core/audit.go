package core

import (
	"database/sql"
	"log"
	"net"
	"net/http"
)

func ClientIP(r *http.Request) string {
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return r.RemoteAddr
	}
	return ip
}
func AuditTx(tx *sql.Tx, r *http.Request, action, kind string, id int64, detail any) error {
	_, e := tx.Exec("INSERT INTO audit_log(at,user_id,action,target_type,target_id,detail,ip) VALUES(?,?,?,?,?,?,?)", Now(), CurrentUser(r).ID, action, kind, id, JS(detail), ClientIP(r))
	return e
}
func (a *Core) Audit(r *http.Request, action, kind string, id int64, detail any) {
	a.AuditUser(CurrentUser(r).ID, ClientIP(r), action, kind, id, detail)
}
func (a *Core) AuditUser(uid int64, ip, action, kind string, id int64, detail any) {
	if _, e := a.DB.Exec("INSERT INTO audit_log(at,user_id,action,target_type,target_id,detail,ip) VALUES(?,?,?,?,?,?,?)", Now(), uid, action, kind, id, JS(detail), ip); e != nil {
		log.Print("写入操作日志失败：", e)
	}
}
