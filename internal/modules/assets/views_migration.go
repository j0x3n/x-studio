package assets

import "database/sql"

func migrateViews(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE assets ADD COLUMN view_code TEXT NOT NULL DEFAULT '';
ALTER TABLE assets ADD COLUMN suggest TEXT NOT NULL DEFAULT '{}';
ALTER TABLE assets ADD COLUMN enhance_check TEXT NOT NULL DEFAULT '{}';
UPDATE assets SET view_code=code WHERE code<>'';
WITH RECURSIVE ancestors(id,view_code,origin,path) AS (
 SELECT parent_id,view_code,id,printf('/%d/',id) FROM assets WHERE view_code<>'' AND parent_id IS NOT NULL
 UNION ALL SELECT parent.parent_id,ancestors.view_code,ancestors.origin,ancestors.path||parent.id||'/' FROM assets parent JOIN ancestors ON parent.id=ancestors.id WHERE parent.parent_id IS NOT NULL AND instr(ancestors.path,printf('/%d/',parent.id))=0
) UPDATE assets SET view_code=coalesce((SELECT view_code FROM ancestors WHERE ancestors.id=assets.id ORDER BY origin DESC LIMIT 1),'') WHERE view_code='';
WITH RECURSIVE inheritance(id,view_code,path) AS (
 SELECT id,view_code,printf('/%d/',id) FROM assets WHERE view_code<>''
 UNION ALL SELECT child.id,parent.view_code,parent.path||child.id||'/' FROM assets child JOIN inheritance parent ON child.parent_id=parent.id WHERE child.view_code='' AND instr(parent.path,printf('/%d/',child.id))=0
) UPDATE assets SET view_code=coalesce((SELECT view_code FROM inheritance WHERE inheritance.id=assets.id LIMIT 1),'') WHERE view_code='';
CREATE INDEX assets_view ON assets(product_id,view_code,archived);
CREATE TRIGGER assets_view_insert AFTER INSERT ON assets BEGIN
 UPDATE assets SET view_code=CASE WHEN NEW.code<>'' THEN NEW.code WHEN NEW.view_code<>'' THEN NEW.view_code ELSE coalesce((SELECT view_code FROM assets WHERE id=NEW.parent_id AND product_id=NEW.product_id),'') END WHERE id=NEW.id;
END;
CREATE TRIGGER assets_view_code AFTER UPDATE OF code ON assets WHEN NEW.code<>'' BEGIN
 UPDATE assets SET view_code=NEW.code,suggest='{}',enhance_check=CASE WHEN enhance_check<>'{}' THEN json_set(enhance_check,'$.pending',json('false')) ELSE enhance_check END WHERE id=NEW.id;
END;
CREATE TRIGGER assets_view_parent AFTER UPDATE OF parent_id ON assets WHEN NEW.code='' AND NEW.view_code='' AND NEW.parent_id IS NOT NULL BEGIN
 UPDATE assets SET view_code=coalesce((SELECT view_code FROM assets WHERE id=NEW.parent_id AND product_id=NEW.product_id),'') WHERE id=NEW.id;
END;
CREATE TRIGGER assets_view_inherit AFTER UPDATE OF view_code ON assets WHEN OLD.view_code='' AND NEW.view_code<>'' BEGIN
 UPDATE assets SET view_code=NEW.view_code WHERE id IN (WITH RECURSIVE parents(id,path) AS (
  SELECT NEW.parent_id,printf('/%d/',NEW.id)
  UNION ALL SELECT a.parent_id,parents.path||a.id||'/' FROM assets a JOIN parents ON a.id=parents.id WHERE a.parent_id IS NOT NULL AND a.product_id=NEW.product_id AND instr(parents.path,printf('/%d/',a.id))=0
 ) SELECT id FROM parents) AND product_id=NEW.product_id AND view_code='';
 UPDATE assets SET view_code=NEW.view_code WHERE id IN (WITH RECURSIVE children(id,path) AS (
  SELECT NEW.id,printf('/%d/',NEW.id)
  UNION ALL SELECT a.id,children.path||a.id||'/' FROM assets a JOIN children ON a.parent_id=children.id WHERE a.product_id=NEW.product_id AND a.view_code='' AND instr(children.path,printf('/%d/',a.id))=0
 ) SELECT id FROM children) AND view_code='';
END;`)
	return err
}

func migrateGlobalModels(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE shop_models_new(
 id INTEGER PRIMARY KEY,shop_id INTEGER REFERENCES shops(id),name TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',archived INTEGER NOT NULL DEFAULT 0,
 cover_reference_id INTEGER REFERENCES model_references(id),created_by INTEGER NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
INSERT INTO shop_models_new SELECT * FROM shop_models;
DROP TABLE shop_models;
ALTER TABLE shop_models_new RENAME TO shop_models;
CREATE INDEX shop_models_shop ON shop_models(shop_id,archived);
CREATE TRIGGER model_identity_immutable BEFORE UPDATE OF id ON shop_models WHEN NEW.id<>OLD.id BEGIN SELECT RAISE(ABORT,'model identity is immutable'); END;`)
	return err
}
