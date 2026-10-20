package core

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
)

type ItemCodeRule struct {
	Prefix    string `json:"prefix"`
	Date      string `json:"date"`
	Separator string `json:"separator"`
	Digits    int    `json:"digits"`
	Start     int64  `json:"start"`
}

func DefaultItemCodeRule() ItemCodeRule {
	return ItemCodeRule{Prefix: "WL", Date: "none", Digits: 6, Start: 1}
}

var itemCodePrefix = regexp.MustCompile(`^[A-Za-z0-9_-]{0,16}$`)

func ParseItemCodeRule(raw json.RawMessage) (ItemCodeRule, error) {
	var fields Object
	var rule ItemCodeRule
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 5 || json.Unmarshal(raw, &rule) != nil {
		return rule, fmt.Errorf("编码规则必须包含前缀、日期、分隔符、序号位数和起始序号")
	}
	for _, key := range []string{"prefix", "date", "separator"} {
		if _, ok := fields[key].(string); !ok {
			return rule, fmt.Errorf("编码规则的前缀、日期和分隔符必须是文本")
		}
	}
	if !itemCodePrefix.MatchString(rule.Prefix) {
		return rule, fmt.Errorf("编码前缀最多16位，只能包含英文字母、数字、横线和下划线")
	}
	if rule.Date != "none" && rule.Date != "day" && rule.Date != "month" && rule.Date != "year" {
		return rule, fmt.Errorf("编码日期格式不正确")
	}
	if rule.Separator != "" && rule.Separator != "-" && rule.Separator != "_" {
		return rule, fmt.Errorf("编码分隔符只能为空、横线或下划线")
	}
	for _, key := range []string{"digits", "start"} {
		if n, ok := fields[key].(float64); !ok || n != math.Trunc(n) {
			return rule, fmt.Errorf("序号位数和起始序号必须是整数")
		}
	}
	if rule.Digits < 1 || rule.Digits > 12 || rule.Start < 1 || rule.Start > 999999999999 {
		return rule, fmt.Errorf("序号位数须为1–12，起始序号须为1–999999999999")
	}
	return rule, nil
}
