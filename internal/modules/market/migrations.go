package market

import (
	"database/sql"

	"xstudio/internal/core"
)

func (*Module) Migrations() []core.Migration {
	return []core.Migration{{Version: 1, Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
CREATE TABLE user_api_tokens(
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  scopes TEXT NOT NULL DEFAULT '["market.ingest"]',
  created_at TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT '',
  revoked_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX user_api_tokens_user ON user_api_tokens(user_id);

CREATE TABLE market_snapshots(
  id INTEGER PRIMARY KEY,
  shop_id INTEGER NOT NULL REFERENCES shops(id),
  kind TEXT NOT NULL,
  source TEXT NOT NULL,
  source_url TEXT NOT NULL DEFAULT '',
  captured_at TEXT NOT NULL DEFAULT '',
  payload_hash TEXT NOT NULL,
  "rows" INTEGER NOT NULL DEFAULT 0,
  raw TEXT NOT NULL DEFAULT '',
  created_by INTEGER NOT NULL REFERENCES users(id),
  created_at TEXT NOT NULL,
  UNIQUE(shop_id, kind, payload_hash)
);

CREATE TABLE market_keywords(
  id INTEGER PRIMARY KEY,
  shop_id INTEGER NOT NULL REFERENCES shops(id),
  category TEXT NOT NULL DEFAULT '',
  word TEXT NOT NULL,
  date TEXT NOT NULL,
  search_volume INTEGER,
  clicks INTEGER,
  ctr_bps INTEGER,
  cvr_bps INTEGER,
  competition INTEGER,
  snapshot_id INTEGER REFERENCES market_snapshots(id),
  updated_at TEXT NOT NULL,
  UNIQUE(shop_id, word, date)
);
CREATE INDEX market_keywords_shop_date ON market_keywords(shop_id, date);

CREATE TABLE competitor_items(
  id INTEGER PRIMARY KEY,
  shop_id INTEGER NOT NULL REFERENCES shops(id),
  platform_item_id TEXT NOT NULL,
  keyword TEXT NOT NULL DEFAULT '',
  rank INTEGER,
  title TEXT NOT NULL DEFAULT '',
  price_cents INTEGER,
  sales_text TEXT,
  sales INTEGER,
  shop_name TEXT NOT NULL DEFAULT '',
  image_url TEXT NOT NULL DEFAULT '',
  material TEXT NOT NULL DEFAULT '',
  params TEXT NOT NULL DEFAULT '{}',
  date TEXT NOT NULL,
  snapshot_id INTEGER REFERENCES market_snapshots(id),
  UNIQUE(shop_id, platform_item_id, keyword, date)
);
CREATE INDEX competitor_items_shop_date ON competitor_items(shop_id, date);

CREATE TABLE listing_daily(
  id INTEGER PRIMARY KEY,
  listing_id INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
  date TEXT NOT NULL,
  impressions INTEGER,
  clicks INTEGER,
  add_cart INTEGER,
  orders INTEGER,
  gmv_cents INTEGER,
  uv INTEGER,
  search_terms TEXT NOT NULL DEFAULT '[]',
  source TEXT NOT NULL,
  snapshot_id INTEGER REFERENCES market_snapshots(id),
  UNIQUE(listing_id, date)
);

CREATE TABLE ad_daily(
  id INTEGER PRIMARY KEY,
  shop_id INTEGER NOT NULL REFERENCES shops(id),
  listing_id INTEGER REFERENCES listings(id) ON DELETE SET NULL,
  date TEXT NOT NULL,
  campaign TEXT NOT NULL DEFAULT '',
  keyword TEXT NOT NULL DEFAULT '',
  cost_cents INTEGER,
  impressions INTEGER,
  clicks INTEGER,
  orders INTEGER,
  gmv_cents INTEGER,
  snapshot_id INTEGER REFERENCES market_snapshots(id)
);
CREATE UNIQUE INDEX ad_daily_key ON ad_daily(shop_id, date, campaign, keyword, coalesce(listing_id, 0));
CREATE INDEX ad_daily_shop_date ON ad_daily(shop_id, date);

CREATE TABLE market_shop_settings(
  shop_id INTEGER PRIMARY KEY REFERENCES shops(id),
  daily_budget_cents INTEGER,
  updated_by INTEGER,
  updated_at TEXT NOT NULL
);

CREATE TABLE listing_title_history(
  id INTEGER PRIMARY KEY,
  listing_id INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
  old_title TEXT NOT NULL DEFAULT '',
  new_title TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  created_by INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX listing_title_history_listing ON listing_title_history(listing_id, created_at);
`)
		return err
	}}}
}
