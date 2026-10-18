package assistant

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
	"xstudio/internal/core"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatContext struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	Selection *struct {
		Type string  `json:"type"`
		IDs  []int64 `json:"ids"`
	} `json:"selection"`
}
type chatRequest struct {
	Messages []message    `json:"messages"`
	Context  *chatContext `json:"context"`
}
type toolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}
type aiResponse struct {
	ToolCalls json.RawMessage   `json:"tool_calls"`
	Reply     string            `json:"reply"`
	Actions   []json.RawMessage `json:"actions"`
	Refs      []json.RawMessage `json:"refs"`
}

func (a *Module) prompt(u core.User, ctx *chatContext) (string, error) {
	b, err := os.ReadFile(filepath.Join(a.Cfg.PromptDir, "助手.md"))
	if err != nil {
		return "", fmt.Errorf("缺少提示词 助手.md")
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return "", fmt.Errorf("无法加载上海时区")
	}
	now := time.Now().In(loc)
	days := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	shopQuery := "SELECT s.name FROM shops s"
	shopArgs := []any{}
	if u.Role.Key != "admin" {
		if u.Role.Scope == "shops" {
			shopQuery += " WHERE EXISTS(SELECT 1 FROM user_shops us WHERE us.shop_id=s.id AND us.user_id=?)"
			shopArgs = append(shopArgs, u.ID)
		} else {
			shopQuery += " WHERE 0"
		}
	}
	shops, err := core.QueryObjects(a.DB, shopQuery+" ORDER BY s.id", shopArgs...)
	if err != nil {
		return "", fmt.Errorf("读取用户店铺失败")
	}
	shopNames := []string{}
	for _, s := range shops {
		shopNames = append(shopNames, s["name"].(string))
	}
	user := fmt.Sprintf("%s；角色：%s；分到的店铺：%s", u.DisplayName, u.Role.Name, strings.Join(shopNames, "、"))
	context := "未提供当前页面和选中对象。"
	if ctx != nil {
		context = fmt.Sprintf("页面路径：%s\n标题：%s\n选中对象：", ctx.Path, ctx.Title)
		selected := []string{}
		if ctx.Selection != nil {
			for _, id := range ctx.Selection.IDs {
				_, label, ok := a.reference(u, ctx.Selection.Type, id)
				if ok {
					selected = append(selected, fmt.Sprintf("%s #%d %s", ctx.Selection.Type, id, label))
				}
			}
		}
		if len(selected) == 0 {
			context += "无"
		} else {
			context += strings.Join(selected, "、")
		}
	}
	return strings.NewReplacer("{{today}}", now.Format("2006-01-02")+" "+days[now.Weekday()]+" "+now.Format("15:04"), "{{user}}", user, "{{tools}}", catalogText(u, toolCatalog), "{{actions}}", actionText(u), "{{context}}", context).Replace(string(b)), nil
}

func validateRequest(in chatRequest) string {
	if len(in.Messages) < 1 || len(in.Messages) > 20 {
		return "对话消息必须为 1–20 条"
	}
	for _, m := range in.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return "消息角色只能是 user 或 assistant"
		}
		if n := utf8.RuneCountInString(m.Content); n < 1 || n > 2000 {
			return "每条消息内容必须为 1–2000 字"
		}
	}
	if in.Messages[len(in.Messages)-1].Role != "user" {
		return "最后一条消息必须来自 user"
	}
	if in.Context != nil {
		if utf8.RuneCountInString(in.Context.Path) > 300 {
			return "页面路径最多 300 字"
		}
		if utf8.RuneCountInString(in.Context.Title) > 100 {
			return "页面标题最多 100 字"
		}
	}
	if in.Context != nil && in.Context.Selection != nil {
		s := in.Context.Selection
		if s.Type != "product" && s.Type != "listing" && s.Type != "asset" {
			return "选中对象类型只能是 product、listing 或 asset"
		}
		if len(s.IDs) > 50 {
			return "选中对象最多 50 个"
		}
	}
	return ""
}

func (a *Module) chat(w http.ResponseWriter, r *http.Request) {
	var in chatRequest
	if !core.Decode(w, r, &in) {
		return
	}
	if err := validateRequest(in); err != "" {
		core.Fail(w, 400, err)
		return
	}
	u := core.CurrentUser(r)
	prompt, err := a.prompt(u, in.Context)
	if err != nil {
		core.Fail(w, 500, err.Error())
		return
	}
	history := make([]object, 0, len(in.Messages)+12)
	chars := 0
	for _, m := range in.Messages {
		history = append(history, object{"role": m.Role, "content": m.Content})
		chars += utf8.RuneCountInString(m.Content)
	}
	ctx := core.WithFeature(core.WithAIActor(r.Context(), core.AIActor{UserID: u.ID}), "assistant")
	tools := []string{}
	for round := 1; round <= 6; round++ {
		if round == 6 {
			history = append(history, object{"role": "user", "content": "工具调用次数已用完，必须现在用已有信息给出 reply"})
		}
		var res aiResponse
		if err = a.AIJSON(ctx, a.Cfg.TextModel, prompt, core.JS(history), &res); err != nil {
			core.Fail(w, 502, err.Error())
			return
		}
		if len(res.ToolCalls) > 0 && string(res.ToolCalls) != "null" {
			if round == 6 {
				core.Fail(w, 502, "工具调用次数已用完，AI 未返回答复")
				return
			}
			var calls []toolCall
			if json.Unmarshal(res.ToolCalls, &calls) != nil || len(calls) < 1 || len(calls) > 4 {
				history = append(history, object{"role": "tool", "content": "工具调用格式不正确，每轮必须为 1–4 个调用"})
				continue
			}
			history = append(history, object{"role": "assistant", "content": core.JS(object{"tool_calls": calls})})
			for _, call := range calls {
				tools = append(tools, call.Name)
				result, e := a.tool(u, call.Name, call.Args)
				if e != nil {
					result = object{"error": toolError(e)}
				}
				history = append(history, object{"role": "tool", "name": call.Name, "content": clip(core.JS(result), 6000)})
			}
			continue
		}
		reply := res.Reply
		if strings.TrimSpace(reply) == "" {
			reply = "我没能给出答复，换个说法试试。"
		}
		actions := []Action{}
		types := []string{}
		dropped := 0
		for _, raw := range res.Actions {
			card, ok := a.validateAction(u, raw)
			if !ok || len(actions) >= 4 {
				dropped++
				continue
			}
			actions = append(actions, card)
			types = append(types, card.Type)
		}
		refs := []Ref{}
		for _, raw := range res.Refs {
			var ref Ref
			if json.Unmarshal(raw, &ref) != nil || (ref.Type != "product" && ref.Type != "listing" && ref.Type != "asset" && ref.Type != "job") {
				continue
			}
			path, label, ok := a.reference(u, ref.Type, ref.ID)
			if ok {
				ref.Path = path
				ref.Label = label
				refs = append(refs, ref)
			}
		}
		a.Audit(r, "assistant.chat", "user", u.ID, object{"rounds": round, "tools": tools, "actions": types, "chars": chars})
		core.WriteJSON(w, 200, object{"reply": reply, "actions": actions, "refs": refs, "dropped": dropped, "rounds": round})
		return
	}
}

func toolError(err error) string {
	// Database errors can contain schema details; expose only deliberate Chinese
	// validation/permission errors as tool output.
	s := err.Error()
	for _, r := range s {
		if r > 127 {
			return s
		}
	}
	return "读取工具数据失败"
}
