package products

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"xstudio/internal/core"
)

func (a *Module) migrateCategories(tx *sql.Tx) error {
	_, e := tx.Exec(`CREATE TABLE categories(id INTEGER PRIMARY KEY,key TEXT UNIQUE NOT NULL,name TEXT NOT NULL,sort INTEGER NOT NULL DEFAULT 0,disabled INTEGER NOT NULL DEFAULT 0,attrs TEXT NOT NULL DEFAULT '[]',codes TEXT NOT NULL DEFAULT '[]',slots TEXT NOT NULL DEFAULT '{}',rules TEXT NOT NULL DEFAULT '',banned TEXT NOT NULL DEFAULT '[]',default_template TEXT NOT NULL DEFAULT '',created_at TEXT,updated_at TEXT);`)
	if e != nil {
		return e
	}
	slots := a.Slots
	if len(slots) == 0 {
		slots = []byte(`{}`)
	}
	_, e = tx.Exec(`INSERT INTO categories(id,key,name,sort,codes,slots,created_at,updated_at) VALUES(1,'pearl','珍珠',10,?,?,?,?)`, core.JS(pearlCodes()), string(slots), core.Now(), core.Now())
	if e != nil {
		return e
	}
	if e = core.AddColumns(tx, "products", [][2]string{{"category_id", "INTEGER REFERENCES categories(id) DEFAULT 1"}}); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE products SET category_id=1"); e != nil {
		return e
	}
	rows, e := tx.Query("SELECT id,info FROM products")
	if e != nil {
		return e
	}
	type productInfo struct {
		id   int64
		info string
	}
	var items []productInfo
	for rows.Next() {
		var v productInfo
		if e = rows.Scan(&v.id, &v.info); e != nil {
			rows.Close()
			return e
		}
		items = append(items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range items {
		var info map[string]json.RawMessage
		if e = json.Unmarshal([]byte(v.info), &info); e != nil {
			return e
		}
		if old, ok := info["category"]; ok {
			if _, exists := info["style"]; !exists {
				info["style"] = old
				delete(info, "category")
				if _, e = tx.Exec("UPDATE products SET info=? WHERE id=?", core.JS(info), v.id); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func pearlCodes() []core.CategoryCode {
	groups := []struct {
		g     string
		names []string
	}{{"A 白底", []string{"一对平放正面", "一对立起 45°", "每个颜色或规格"}}, {"B 微距", []string{"珍珠表面生长纹", "珍珠光泽和反光", "配件做工", "925 印记", "耳针或耳夹结构", "珍珠微瑕"}}, {"C 尺寸", []string{"钢尺参照", "一元硬币参照", "指尖或手心", "同一只耳朵各尺寸上耳"}}, {"D 上耳", []string{"侧脸正对耳朵", "微侧 45°", "正脸半脸", "生活感上耳", "耳饰配颈链"}}, {"E 场景", []string{"深蓝丝绒", "米白亚麻布", "大理石纹场景", "打开的包装盒", "手托托盘批量实拍（不校色）"}}, {"F 包装", []string{"礼盒和品牌卡", "打开的礼盒", "打包好的快递"}}}
	descriptions := map[string][2]string{
		"A1": {"一对平放的正面俯拍，白色或浅色背景，四周留白", "白底图"},
		"A2": {"一对立起来，45°", "白底图备选"},
		"A3": {"每个颜色 / 规格各一张，角度一致", "主图第 5 张（多规格并排）"},
		"B1": {"珍珠表面微距，能看到生长纹", "主图第 3 张、详情页真假屏"},
		"B2": {"珍珠的光泽和反光", "主图第 3 张备选、详情页细节"},
		"B3": {"配件做工特写（银托、碎银、蝴蝶结）", "详情页细节"},
		"B4": {"925 印记特写", "详情页细节"},
		"B5": {"耳针 / 耳夹结构", "详情页细节"},
		"B6": {"珍珠的小瑕疵", "详情页细节"},
		"C1": {"放在钢尺上，看得到刻度", "尺寸"},
		"C2": {"和硬币放一起", "尺寸"},
		"C3": {"放在指尖或手心", "尺寸"},
		"C4": {"同一只耳朵分别戴各个尺寸", "尺寸（最直观）"},
		"D1": {"上耳侧脸，耳朵正对镜头", "首图、上耳屏"},
		"D2": {"上耳微侧 45°", "上耳屏"},
		"D3": {"正脸半脸，两边耳环都入镜", "上耳屏"},
		"D4": {"生活感上耳：撩头发、侧头", "首图第一候选"},
		"D5": {"耳饰和颈链一起戴", "上耳屏"},
		"E1": {"放在深蓝丝绒上", "首图备选、详情页首屏"},
		"E2": {"放在米白亚麻布上", "场景"},
		"E3": {"大理石面配小物", "场景"},
		"E4": {"放在打开的包装盒里", "规格或礼盒"},
		"E5": {"托盘上很多颗散珠或很多对成品一起拍", "主图第 2 张原相机实拍、详情页真假屏"},
		"F1": {"合上的礼盒加品牌卡", "包装"},
		"F2": {"打开的礼盒，饰品在里面", "包装"},
		"F3": {"打包好的快递", "包装"},
	}
	out := []core.CategoryCode{}
	for _, g := range groups {
		for i, n := range g.names {
			code := fmt.Sprintf("%c%d", g.g[0], i+1)
			suffix := ""
			if code == "A3" {
				suffix = "规格"
			}
			if code == "C4" {
				suffix = "尺寸"
			}
			out = append(out, core.CategoryCode{Code: code, Group: g.g, Name: n, Desc: descriptions[code][0], Use: descriptions[code][1], Suffix: suffix})
		}
	}
	return out
}
