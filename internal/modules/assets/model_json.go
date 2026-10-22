package assets

import "encoding/json"

func (m ShopModel) MarshalJSON() ([]byte, error) {
	type model ShopModel
	var shop *int64
	if m.ShopID != 0 {
		shop = &m.ShopID
	}
	return json.Marshal(struct {
		model
		ShopID *int64 `json:"shop_id"`
	}{model: model(m), ShopID: shop})
}
