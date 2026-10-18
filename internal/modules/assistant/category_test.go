package assistant

import (
	"testing"
	"xstudio/internal/core"
)

func TestProductToolsIncludeCategoryName(t *testing.T) {
	f := newFixture(t)
	pid, _ := f.product(t, "水晶商品", 1)
	cid := f.exec(t, "INSERT INTO categories(key,name,created_at,updated_at) VALUES('crystal','水晶',?,?)", core.Now(), core.Now())
	f.exec(t, "UPDATE products SET category_id=? WHERE id=?", cid, pid)
	for _, name := range []string{"get_product", "search_products"} {
		args := object{"id": pid}
		if name == "search_products" {
			args = object{"q": "水晶商品"}
		}
		result := f.tool(t, 1, name, args)
		if name == "search_products" {
			result = result.([]object)[0]
		}
		if result.(object)["category_name"] != "水晶" {
			t.Fatal(result)
		}
	}
}
