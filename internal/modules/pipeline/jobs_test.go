package pipeline

import (
	"fmt"
	"testing"
)

func TestCreativeJobListingVisibility(t *testing.T) {
	f := newFixture(t, nil)
	f.login(t, "admin")
	lid := f.product(t)
	shop := idOf(f.req(t, "POST", "/api/shops", Object{"name": "另一店", "platform": "other", "language": "en"}, 201), "shop")
	f.newUser(t, "creative_reader", 2, 1)
	f.newUser(t, "other_reader", 2, shop)
	if _, err := f.a.DB.Exec(`CREATE TABLE creative_sets(id INTEGER PRIMARY KEY,listing_id INTEGER);
CREATE TABLE creative_items(id INTEGER PRIMARY KEY,set_id INTEGER,listing_id INTEGER);`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.DB.Exec("INSERT INTO creative_sets VALUES(1,?); INSERT INTO creative_items VALUES(1,1,NULL),(2,NULL,?)", lid, lid); err != nil {
		t.Fatal(err)
	}
	for index, target := range []string{"creative_set", "creative_item", "creative_item"} {
		targetID := 1
		if index == 2 {
			targetID = 2
		}
		result, err := f.a.DB.Exec("INSERT INTO jobs(target_id,target_type,user_id,kind,status,created_at,steps) VALUES(?,?,1,'creative.item','done',?,'')", targetID, target, now())
		if err != nil {
			t.Fatal(err)
		}
		jid, _ := result.LastInsertId()
		f.login(t, "creative_reader")
		f.req(t, "GET", fmt.Sprintf("/api/jobs/%d", jid), nil, 200)
		f.login(t, "other_reader")
		f.req(t, "GET", fmt.Sprintf("/api/jobs/%d", jid), nil, 404)
	}
}
