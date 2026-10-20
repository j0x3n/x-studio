package assembly

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
	"xstudio/internal/core"
	"xstudio/internal/modules/inventory"
)

type RecipeLineInput struct {
	ItemID  int64 `json:"item_id"`
	Qty     int64 `json:"qty"`
	LossBps int64 `json:"loss_bps"`
}

type RecipeInput struct {
	Name         *string            `json:"name"`
	OutputItemID *int64             `json:"output_item_id"`
	OutputQty    *int64             `json:"output_qty"`
	Currency     *string            `json:"currency"`
	LaborCost    *int64             `json:"labor_cost"`
	ArticleID    *int64             `json:"article_id"`
	Note         *string            `json:"note"`
	Archived     *bool              `json:"archived"`
	Lines        *[]RecipeLineInput `json:"lines"`
}

func (a *Module) listRecipes(w http.ResponseWriter, r *http.Request) (any, error) {
	u := core.CurrentUser(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	archivedFilter := r.URL.Query().Get("archived")

	where := []string{"1=1"}
	args := []any{}

	if archivedFilter == "1" {
		where = append(where, "r.archived=1")
	} else if archivedFilter == "all" {
		// show all
	} else {
		where = append(where, "r.archived=0")
	}

	if q != "" {
		where = append(where, "(r.name LIKE ? OR i.code LIKE ? OR i.name LIKE ?)")
		pat := "%" + q + "%"
		args = append(args, pat, pat, pat)
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countQuery := "SELECT count(*) FROM assembly_recipes r JOIN inv_items i ON i.id=r.output_item_id WHERE " + whereClause
	if err := a.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	offset, size := core.Page(r)
	query := `SELECT 
		r.id, r.name, r.output_item_id, r.output_qty, r.currency, r.labor_cost, r.article_id, r.note,
		r.archived, r.created_by, r.created_at, r.updated_at,
		i.code AS output_item_code, i.name AS output_item_name, i.unit AS output_item_unit,
		ka.title AS article_title,
		(SELECT count(*) FROM assembly_recipe_lines l WHERE l.recipe_id=r.id) AS lines_count
	FROM assembly_recipes r
	JOIN inv_items i ON i.id = r.output_item_id
	LEFT JOIN kb_articles ka ON ka.id = r.article_id AND ka.deleted_at IS NULL
	WHERE ` + whereClause + ` ORDER BY r.id DESC LIMIT ? OFFSET ?`

	queryArgs := append(args, size, offset)
	rows, err := objects(a.DB, query, queryArgs...)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if arch, ok := row["archived"].(int64); ok {
			row["archived"] = arch != 0
		}
		redactRecipe(u, row)
	}

	return core.Object{"items": rows, "total": total}, nil
}

func (a *Module) getRecipe(w http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(400, "无效的配方ID")
	}
	u := core.CurrentUser(r)
	rec, err := a.fetchRecipeDetail(r.Context(), a.DB, id, u)
	if err != nil {
		return nil, err
	}
	return core.Object{"recipe": rec}, nil
}

func (a *Module) fetchRecipeDetail(ctx context.Context, q inventory.Querier, id int64, u core.User) (core.Object, error) {
	query := `SELECT 
		r.id, r.name, r.output_item_id, r.output_qty, r.currency, r.labor_cost, r.article_id, r.note,
		r.archived, r.created_by, r.created_at, r.updated_at,
		i.code AS output_item_code, i.name AS output_item_name, i.unit AS output_item_unit,
		ka.title AS article_title
	FROM assembly_recipes r
	JOIN inv_items i ON i.id = r.output_item_id
	LEFT JOIN kb_articles ka ON ka.id = r.article_id AND ka.deleted_at IS NULL
	WHERE r.id=?`

	rec, err := one(q, query, id)
	if err != nil {
		return nil, err
	}
	if arch, ok := rec["archived"].(int64); ok {
		rec["archived"] = arch != 0
	}

	linesQuery := `SELECT 
		l.id, l.recipe_id, l.item_id, l.qty, l.loss_bps,
		i.code AS item_code, i.name AS item_name, i.unit AS item_unit
	FROM assembly_recipe_lines l
	JOIN inv_items i ON i.id = l.item_id
	WHERE l.recipe_id=? ORDER BY l.id ASC`

	lines, err := objects(q, linesQuery, id)
	if err != nil {
		return nil, err
	}
	rec["lines"] = lines
	redactRecipe(u, rec)
	return rec, nil
}

func (a *Module) validateItem(q inventory.Querier, itemID int64, label string) error {
	var archived int
	err := q.QueryRow("SELECT archived FROM inv_items WHERE id=?", itemID).Scan(&archived)
	if errors.Is(err, sql.ErrNoRows) {
		return bad(404, label+"不存在")
	}
	if err != nil {
		return err
	}
	if archived == 1 {
		return bad(409, label+"已归档")
	}
	return nil
}

func (a *Module) validateArticle(q inventory.Querier, u core.User, articleID *int64) error {
	if articleID == nil || *articleID <= 0 {
		return nil
	}
	if u.Role.Key != "admin" && !u.Has(core.KBView) {
		return bad(403, "没有知识库查看权限，不能关联文章")
	}
	var count int
	err := q.QueryRow("SELECT count(*) FROM kb_articles WHERE id=? AND deleted_at IS NULL", *articleID).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return bad(400, "知识库文章不存在")
	}
	return nil
}

func (a *Module) validateLines(q inventory.Querier, outputItemID int64, lines []RecipeLineInput) error {
	if len(lines) == 0 || len(lines) > 100 {
		return bad(400, "配方需要 1–100 种原料")
	}
	seen := make(map[int64]bool)
	for _, l := range lines {
		if l.ItemID <= 0 {
			return bad(400, "原料物料参数不正确")
		}
		if l.ItemID == outputItemID {
			return bad(400, "原料物料不能与产物相同")
		}
		if seen[l.ItemID] {
			return bad(400, "原料列表中存在重复物料")
		}
		seen[l.ItemID] = true
		if l.Qty <= 0 {
			return bad(400, "原料用量必须大于0")
		}
		if l.LossBps < 0 || l.LossBps > 10000 {
			return bad(400, "损耗万分比必须在 0 到 10000 之间")
		}
		if err := a.validateItem(q, l.ItemID, "原料物料"); err != nil {
			return err
		}
	}
	return nil
}

func (a *Module) createRecipe(w http.ResponseWriter, r *http.Request) (any, error) {
	u := core.CurrentUser(r)
	var in struct {
		Name         string            `json:"name"`
		OutputItemID int64             `json:"output_item_id"`
		OutputQty    int64             `json:"output_qty"`
		Currency     string            `json:"currency"`
		LaborCost    int64             `json:"labor_cost"`
		ArticleID    *int64            `json:"article_id"`
		Note         string            `json:"note"`
		Lines        []RecipeLineInput `json:"lines"`
	}
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 || utf8.RuneCountInString(in.Note) > 4000 {
		return nil, bad(400, "配方名称需要 1–100 字，备注最多 4000 字")
	}
	if in.OutputItemID <= 0 {
		return nil, bad(400, "产出物料不正确")
	}
	if in.OutputQty <= 0 {
		return nil, bad(400, "单次配方产出数量必须大于0")
	}

	cur := strings.TrimSpace(in.Currency)
	if cur == "" {
		cur = "CNY"
	}
	if len(cur) != 3 || strings.Trim(cur, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return nil, bad(400, "币种应为三个大写字母")
	}

	if in.LaborCost < 0 {
		return nil, bad(400, "工费不能为负数")
	}
	if in.LaborCost > 0 && !costEditAllowed(u) {
		return nil, bad(403, "没有设置工费的权限")
	}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := a.validateItem(tx, in.OutputItemID, "产出物料"); err != nil {
		return nil, err
	}
	if err := a.validateArticle(tx, u, in.ArticleID); err != nil {
		return nil, err
	}
	if err := a.validateLines(tx, in.OutputItemID, in.Lines); err != nil {
		return nil, err
	}

	now := core.Now()
	var articleVal any
	if in.ArticleID != nil && *in.ArticleID > 0 {
		articleVal = *in.ArticleID
	}

	res, err := tx.Exec(`INSERT INTO assembly_recipes(name, output_item_id, output_qty, currency, labor_cost, article_id, note, archived, created_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		name, in.OutputItemID, in.OutputQty, cur, in.LaborCost, articleVal, in.Note, 0, u.ID, now, now)
	if err != nil {
		return nil, err
	}

	recipeID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	for _, line := range in.Lines {
		_, err = tx.Exec(`INSERT INTO assembly_recipe_lines(recipe_id, item_id, qty, loss_bps, created_at)
			VALUES(?,?,?,?,?)`,
			recipeID, line.ItemID, line.Qty, line.LossBps, now)
		if err != nil {
			return nil, err
		}
	}

	if err = core.AuditTx(tx, r, "assembly.recipe_create", "assembly_recipe", recipeID, core.Object{
		"name":           name,
		"output_item_id": in.OutputItemID,
		"output_qty":     in.OutputQty,
		"lines_count":    len(in.Lines),
	}); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	rec, err := a.fetchRecipeDetail(r.Context(), a.DB, recipeID, u)
	if err != nil {
		return nil, err
	}
	return core.Object{"recipe": rec}, nil
}

func (a *Module) patchRecipe(w http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(400, "无效的配方ID")
	}
	u := core.CurrentUser(r)

	var in RecipeInput
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Fetch current recipe
	var currName, currCur, currNote, currCreated, currUpdated string
	var currOutputItemID, currOutputQty, currLaborCost, currCreatedBy int64
	var currArticleID sql.NullInt64
	var currArchived int

	err = tx.QueryRow(`SELECT name, output_item_id, output_qty, currency, labor_cost, article_id, note, archived, created_by, created_at, updated_at
		FROM assembly_recipes WHERE id=?`, id).Scan(
		&currName, &currOutputItemID, &currOutputQty, &currCur, &currLaborCost, &currArticleID, &currNote, &currArchived, &currCreatedBy, &currCreated, &currUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "配方不存在")
	}
	if err != nil {
		return nil, err
	}

	name := currName
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return nil, bad(400, "配方名称不能为空")
		}
		name = n
	}

	outputItemID := currOutputItemID
	if in.OutputItemID != nil {
		if *in.OutputItemID <= 0 {
			return nil, bad(400, "产出物料不正确")
		}
		outputItemID = *in.OutputItemID
		if err := a.validateItem(tx, outputItemID, "产出物料"); err != nil {
			return nil, err
		}
	}

	outputQty := currOutputQty
	if in.OutputQty != nil {
		if *in.OutputQty <= 0 {
			return nil, bad(400, "产出数量必须大于0")
		}
		outputQty = *in.OutputQty
	}

	currency := currCur
	if in.Currency != nil {
		c := strings.TrimSpace(*in.Currency)
		if len(c) != 3 || strings.Trim(c, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			return nil, bad(400, "币种应为三个大写字母")
		}
		if c != currCur && !costEditAllowed(u) {
			return nil, bad(403, "没有修改成本币种的权限")
		}
		currency = c
	}

	laborCost := currLaborCost
	if in.LaborCost != nil {
		if *in.LaborCost < 0 {
			return nil, bad(400, "工费不能为负数")
		}
		if *in.LaborCost != currLaborCost && !costEditAllowed(u) {
			return nil, bad(403, "没有修改工费的权限")
		}
		laborCost = *in.LaborCost
	}

	articleID := in.ArticleID
	if articleID != nil {
		if err := a.validateArticle(tx, u, articleID); err != nil {
			return nil, err
		}
	}

	note := currNote
	if in.Note != nil {
		note = *in.Note
	}
	if utf8.RuneCountInString(name) > 100 || utf8.RuneCountInString(note) > 4000 {
		return nil, bad(400, "配方名称最多 100 字，备注最多 4000 字")
	}
	// Revalidate existing lines when changing only the output item.
	if in.Lines == nil && outputItemID != currOutputItemID {
		var conflicts int
		if err = tx.QueryRow("SELECT count(*) FROM assembly_recipe_lines WHERE recipe_id=? AND item_id=?", id, outputItemID).Scan(&conflicts); err != nil {
			return nil, err
		}
		if conflicts > 0 {
			return nil, bad(400, "原料物料不能与产物相同")
		}
	}

	archived := currArchived
	if in.Archived != nil {
		if *in.Archived {
			archived = 1
		} else {
			archived = 0
		}
	}

	// Update recipe
	var articleVal any
	if articleID != nil {
		if *articleID > 0 {
			articleVal = *articleID
		} else {
			articleVal = nil
		}
	} else if currArticleID.Valid {
		articleVal = currArticleID.Int64
	}

	now := core.Now()
	_, err = tx.Exec(`UPDATE assembly_recipes
		SET name=?, output_item_id=?, output_qty=?, currency=?, labor_cost=?, article_id=?, note=?, archived=?, updated_at=?
		WHERE id=?`,
		name, outputItemID, outputQty, currency, laborCost, articleVal, note, archived, now, id)
	if err != nil {
		return nil, err
	}

	// If lines updated
	if in.Lines != nil {
		if err := a.validateLines(tx, outputItemID, *in.Lines); err != nil {
			return nil, err
		}
		if _, err = tx.Exec("DELETE FROM assembly_recipe_lines WHERE recipe_id=?", id); err != nil {
			return nil, err
		}
		for _, line := range *in.Lines {
			_, err = tx.Exec(`INSERT INTO assembly_recipe_lines(recipe_id, item_id, qty, loss_bps, created_at)
				VALUES(?,?,?,?,?)`,
				id, line.ItemID, line.Qty, line.LossBps, now)
			if err != nil {
				return nil, err
			}
		}
	}

	auditAction := "assembly.recipe_update"
	if in.Archived != nil && *in.Archived != (currArchived == 1) {
		if *in.Archived {
			auditAction = "assembly.recipe_archive"
		} else {
			auditAction = "assembly.recipe_unarchive"
		}
	}

	if err = core.AuditTx(tx, r, auditAction, "assembly_recipe", id, core.Object{"name": name}); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	rec, err := a.fetchRecipeDetail(r.Context(), a.DB, id, u)
	if err != nil {
		return nil, err
	}
	return core.Object{"recipe": rec}, nil
}

func (a *Module) archiveRecipe(w http.ResponseWriter, r *http.Request) (any, error) {
	id := core.ID(r, "id")
	if id == 0 {
		return nil, bad(400, "无效的配方ID")
	}
	u := core.CurrentUser(r)

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var name string
	err = tx.QueryRow("SELECT name FROM assembly_recipes WHERE id=?", id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, bad(404, "配方不存在")
	}
	if err != nil {
		return nil, err
	}

	now := core.Now()
	if _, err = tx.Exec("UPDATE assembly_recipes SET archived=1, updated_at=? WHERE id=?", now, id); err != nil {
		return nil, err
	}

	if err = core.AuditTx(tx, r, "assembly.recipe_archive", "assembly_recipe", id, core.Object{"name": name}); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	rec, err := a.fetchRecipeDetail(r.Context(), a.DB, id, u)
	if err != nil {
		return nil, err
	}
	return core.Object{"recipe": rec}, nil
}
