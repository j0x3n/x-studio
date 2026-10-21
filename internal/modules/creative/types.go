package creative

import "xstudio/internal/modules/scenes"

type TextSpec struct {
	Key      string `json:"key"`
	From     string `json:"from"`
	Max      int    `json:"max"`
	Critical bool   `json:"critical"`
}
type ExpectedText struct {
	Key      string `json:"key"`
	Text     string `json:"text"`
	Critical bool   `json:"critical"`
}
type ExportSize struct {
	Name string `json:"name"`
	W    int    `json:"w"`
	H    int    `json:"h"`
}
type Wear struct {
	Slot     string `json:"slot"`
	BodyPart string `json:"body_part"`
	Framing  string `json:"framing"`
}
type Template struct {
	ID          int64        `json:"id"`
	Key         string       `json:"key"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Slot        string       `json:"slot"`
	Categories  []string     `json:"categories"`
	Purpose     string       `json:"purpose"`
	Prompt      string       `json:"prompt"`
	StyleRefs   []string     `json:"style_refs"`
	Texts       []TextSpec   `json:"texts"`
	PhotoCodes  []string     `json:"photo_codes"`
	ProductMode string       `json:"product_mode"`
	LockShape   string       `json:"lock_shape"`
	SizeCompare bool         `json:"size_compare"`
	HeroStyle   string       `json:"hero_style"`
	ProductBox  []float64    `json:"product_box"`
	GenSize     string       `json:"gen_size"`
	Export      []ExportSize `json:"export"`
	Count       int          `json:"count"`
	Wear        *Wear        `json:"wear"`
	Version     int          `json:"version"`
	Enabled     bool         `json:"enabled"`
	Source      string       `json:"source"`
}
type Size struct {
	Label string  `json:"label"`
	MM    float64 `json:"mm"`
}
type Params struct {
	SceneID     int64  `json:"scene_id"`
	HeroStyle   string `json:"hero_style"`
	HeroB       *bool  `json:"hero_b"`
	Sizes       []Size `json:"sizes"`
	Note        string `json:"note"`
	SkipMissing bool   `json:"skip_missing"`
}
type Set struct {
	Scene           *SceneSummary     `json:"scene"`
	ID              int64             `json:"id"`
	ListingID       int64             `json:"listing_id"`
	Version         int               `json:"version"`
	Status          string            `json:"status"`
	TemplateKeys    map[string]string `json:"template_keys"`
	Params          Params            `json:"params"`
	Parts           []string          `json:"parts"`
	ImagesUsed      int               `json:"images_used"`
	ImagesAllowance int               `json:"images_allowance"`
	GlancedAt       string            `json:"glanced_at"`
	CreatedBy       int64             `json:"created_by"`
}
type Item struct {
	SceneSnapshot    scenes.Snapshot `json:"scene_snapshot"`
	Scene            *SceneSummary   `json:"scene"`
	ID               int64           `json:"id"`
	SetID            *int64          `json:"set_id"`
	ListingID        int64           `json:"listing_id"`
	Slot             string          `json:"slot"`
	Seq              int             `json:"seq"`
	TemplateKey      string          `json:"template_key"`
	TemplateVersion  int             `json:"template_version"`
	TemplateSnapshot Template        `json:"template_snapshot"`
	Attempt          int             `json:"attempt"`
	AutoAttempt      int             `json:"auto_attempt"`
	Superseded       bool            `json:"superseded"`
	Variant          string          `json:"variant"`
	ProductMode      string          `json:"product_mode"`
	Model            string          `json:"model"`
	ModelReturned    string          `json:"model_returned"`
	SourceAssetIDs   []int64         `json:"source_asset_ids"`
	ModelID          int64           `json:"model_id"`
	ModelRefIDs      []int64         `json:"model_ref_ids"`
	WearType         string          `json:"wear_type"`
	SizeCompare      bool            `json:"size_compare"`
	Prompt           string          `json:"prompt"`
	Texts            []ExpectedText  `json:"texts"`
	OutputAssetID    int64           `json:"output_asset_id"`
	CanvasFile       string          `json:"canvas_file"`
	Hard             Hard            `json:"hard"`
	ReviewA          Review          `json:"review_a"`
	ReviewB          Review          `json:"review_b"`
	Route            string          `json:"route"`
	RouteReasons     []string        `json:"route_reasons"`
	Risk             int             `json:"risk"`
	Human            string          `json:"human"`
	Error            string          `json:"error"`
	CreatedBy        int64           `json:"created_by"`
}
type OCRText struct {
	Text string    `json:"text"`
	Box  []float64 `json:"box"`
}
type TextCheck struct {
	Key      string    `json:"key"`
	Expect   string    `json:"expect"`
	Found    string    `json:"found"`
	Pass     bool      `json:"pass"`
	Warn     bool      `json:"warn"`
	Critical bool      `json:"critical"`
	Box      []float64 `json:"box,omitempty"`
}
type PixelCheck struct {
	Pass    bool `json:"pass"`
	MaxDiff int  `json:"max_diff"`
}
type FormatCheck struct {
	Pass bool `json:"pass"`
}
type Hard struct {
	Cutout *CutoutCheck `json:"cutout,omitempty"`
	Pixel  PixelCheck   `json:"pixel"`
	Text   []TextCheck  `json:"text"`
	Banned []string     `json:"banned"`
	Format FormatCheck  `json:"format"`
	OCR    []OCRText    `json:"ocr"`
	Error  string       `json:"error,omitempty"`
}
type CutoutCheck struct {
	Mode     string  `json:"mode"`
	Coverage float64 `json:"coverage"`
	Code     string  `json:"code"`
}
type Issue struct {
	Check  string    `json:"check"`
	Box    []float64 `json:"box"`
	Reason string    `json:"reason"`
}
type Review struct {
	Checks map[string]string `json:"checks"`
	SizeMM []float64         `json:"size_mm"`
	Score  int               `json:"score"`
	Issues []Issue           `json:"issues"`
}
