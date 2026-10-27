package strategyagent

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

func issue(code, path, message string) Issue {
	return Issue{Code: code, Path: path, Message: message, Repairable: true}
}

// CheckRequirements checks extracted structure, never claiming to prove the
// semantic equivalence of a natural-language requirement and the user's words.
func CheckRequirements(card RequirementCard, maxMessageSeq int64) []Issue {
	var issues []Issue
	for _, part := range []struct{ name, brief string }{{"stage1Brief", card.Stage1Brief}, {"stage2Brief", card.Stage2Brief}, {"stage3Brief", card.Stage3Brief}} {
		if strings.TrimSpace(part.brief) == "" {
			issues = append(issues, issue("missing_requirement", "requirements."+part.name, "完整需求不能为空；没有限制时也要明确写出。"))
		}
	}
	if len(card.OpenQuestions) > 0 {
		issues = append(issues, issue("open_questions", "requirements.openQuestions", "仍有待确认问题，不能进入确认。"))
	}
	if card.CoveredMessageSeq != maxMessageSeq {
		issues = append(issues, issue("message_coverage", "requirements.coveredMessageSeq", "需求必须覆盖本轮消息序号。"))
	}
	if len(card.ExplicitRules) == 0 {
		issues = append(issues, issue("missing_rule_sources", "requirements.explicitRules", "需要保留明确规则及其来源消息。"))
	}
	for i, rule := range card.ExplicitRules {
		if strings.TrimSpace(rule.Text) == "" || rule.SourceMessageSeq < 1 || rule.SourceMessageSeq > maxMessageSeq {
			issues = append(issues, issue("invalid_rule_source", fmt.Sprintf("requirements.explicitRules[%d]", i), "规则必须引用本轮已加载的用户消息。"))
		}
	}
	return issues
}

// CheckSpec validates deterministic structure and supported domain operations.
// Unsupported consumer semantics return issues rather than passing by default.
func CheckSpec(spec StrategySpec, snapshot Snapshot) []Issue {
	var issues []Issue
	if spec.Form == nil || spec.Scoring == nil || spec.Recommendation == nil || spec.Display == nil {
		return []Issue{issue("incomplete_spec", "spec", "form、scoring、recommendation、display 必须全部提供。")}
	}
	fields := make(map[string]Field)
	for _, group := range []struct {
		path   string
		values []Field
	}{{"form.intake.fields", spec.Form.Intake.Fields}, {"scoring.scoreInput.fields", spec.Scoring.ScoreInput.Fields}} {
		if len(group.values) == 0 {
			issues = append(issues, issue("missing_fields", group.path, "字段列表不能为空。"))
		}
		for i, field := range group.values {
			location := fmt.Sprintf("%s[%d]", group.path, i)
			if field.Key == "" || field.Label == "" {
				issues = append(issues, issue("invalid_field", location, "字段 key 和 label 必须填写。"))
			}
			if _, exists := fields[field.Key]; exists {
				issues = append(issues, issue("duplicate_field", location, "字段 key 必须全局唯一。"))
			}
			fields[field.Key] = field
			if !oneOf(field.Type, "text", "number", "select", "searchSelect", "enum") {
				issues = append(issues, issue("invalid_field_type", location, "不支持的字段类型。"))
			}
			if group.path == "scoring.scoreInput.fields" && field.Type != "number" {
				issues = append(issues, issue("invalid_score_field", location, "成绩字段必须是 number。"))
			}
			if field.Type == "number" && (field.Min == nil || field.Max == nil || !finite(*field.Min) || !finite(*field.Max) || *field.Min > *field.Max) {
				issues = append(issues, issue("invalid_range", location, "数值字段需要明确且有序的 min/max，不能猜范围。"))
			}
			if field.DataRef != "" && !oneOf(field.DataRef, "grades", "middleSchools", "districts") {
				issues = append(issues, issue("invalid_data_ref", location, "未知 dataRef。"))
			}
			if field.DataRef != "" && len(field.Options) > 0 {
				issues = append(issues, issue("mixed_options", location, "dataRef 与 options 不能同时出现。"))
			}
			if field.Type == "searchSelect" && field.DataRef == "" {
				issues = append(issues, issue("missing_data_ref", location, "searchSelect 需要 dataRef。"))
			}
			if field.Type == "enum" && len(field.OptionsEnum) == 0 {
				issues = append(issues, issue("missing_options", location, "enum 需要完整 optionsEnum。"))
			}
			if (field.Type == "select" || field.Type == "searchSelect") && field.DataRef == "" && len(field.Options) == 0 {
				issues = append(issues, issue("missing_options", location, "选择字段需要选项或数据源。"))
			}
			if field.DataRef != "" && len(snapshot.Catalog[field.DataRef]) == 0 {
				issues = append(issues, issue("missing_catalog", location, "快照没有对应的数据源。"))
			}
			seen := make(map[string]bool)
			for _, option := range field.OptionsEnum {
				if option.ID == "" || option.Label == "" || seen[option.ID] {
					issues = append(issues, issue("invalid_enum", location, "枚举 id/label 必填且 id 唯一。"))
				}
				seen[option.ID] = true
			}
		}
	}
	for i, rule := range spec.Form.Eligibility.Rules {
		p := fmt.Sprintf("form.eligibility.rules[%d]", i)
		issues = append(issues, checkCondition(rule.Condition, fields, p+".condition", 0)...)
		if !oneOf(rule.Action, "redirect", "toast", "pass") || (rule.Action != "pass" && rule.Message == "") {
			issues = append(issues, issue("invalid_action", p, "排除使用 redirect，可修正输入使用 toast，且需要文案。"))
		}
	}
	if !oneOf(spec.Form.Eligibility.DefaultAction, "pass", "block") {
		issues = append(issues, issue("invalid_action", "form.eligibility.defaultAction", "defaultAction 只能是 pass/block。"))
	}
	if len(spec.Scoring.ScoreInput.Validation) > 0 {
		issues = append(issues, issue("unsupported_rule", "scoring.scoreInput.validation", "跨字段校验的真假方向尚未与学生页面验证，本期不允许该形态通过检查。"))
	}
	formulas := make(map[string]Formula)
	scoreFields := make(map[string]Field)
	for _, field := range spec.Scoring.ScoreInput.Fields {
		scoreFields[field.Key] = field
	}
	for i, formula := range spec.Scoring.ScoreCalc {
		p := fmt.Sprintf("scoring.scoreCalc[%d]", i)
		if _, exists := formulas[formula.Name]; exists || formula.Name == "" {
			issues = append(issues, issue("duplicate_formula", p, "公式 name 必须填写且唯一。"))
		}
		formulas[formula.Name] = formula
		if formula.Type != "weighted" || formula.OutputKey != "user_score" || len(formula.Inputs) == 0 {
			issues = append(issues, issue("invalid_formula", p, "公式必须是非空 weighted，outputKey=user_score。"))
		}
		if !finite(formula.Offset) || math.Trunc(formula.Offset) != formula.Offset {
			issues = append(issues, issue("unsupported_precision", p, "小数 lookup 键尚未核对消费者，不允许隐式舍入。"))
		}
		for _, input := range formula.Inputs {
			if _, exists := scoreFields[input.Field]; !exists {
				issues = append(issues, issue("unknown_score_field", p, "公式引用了未知成绩字段。"))
			}
			weight := 1.0
			if input.Weight != nil {
				weight = *input.Weight
			}
			if !finite(weight) || !finite(input.Offset) || math.Trunc(weight) != weight || math.Trunc(input.Offset) != input.Offset {
				issues = append(issues, issue("unsupported_precision", p, "平均数/小数权重需要先确认精度与查询协议，不能用近似权重代替。"))
			}
		}
	}
	if len(formulas) == 0 {
		issues = append(issues, issue("missing_formula", "scoring.scoreCalc", "至少提供一条公式。"))
	}
	issues = append(issues, checkFormulaCoverage(spec.Scoring.ScoreCalc, fields, snapshot)...)
	issues = append(issues, checkTree(*spec.Recommendation, fields, formulas, snapshot)...)
	pathNodes := make(map[string]bool)
	for i, p := range spec.Recommendation.Paths {
		location := fmt.Sprintf("recommendation.paths[%d]", i)
		node, exists := spec.Recommendation.DecisionTree.Nodes[p.RecommendNodeID]
		if !exists || node.Type != "recommend" || pathNodes[p.RecommendNodeID] {
			issues = append(issues, issue("invalid_path", location, "路径必须唯一引用一个 recommend 节点。"))
		}
		pathNodes[p.RecommendNodeID] = true
		issues = append(issues, checkRecommendationPath(p, snapshot, location)...)
	}
	for id, node := range spec.Recommendation.DecisionTree.Nodes {
		if node.Type == "recommend" && node.RenderType != "quota" && !pathNodes[id] {
			issues = append(issues, issue("missing_path", "recommendation.decisionTree.nodes."+id, "推荐节点缺少计算路径。"))
		}
	}
	seenSections := make(map[string]bool)
	for i, section := range spec.Display.Sections {
		location := fmt.Sprintf("display.sections[%d]", i)
		node, exists := spec.Recommendation.DecisionTree.Nodes[section.RecommendNodeID]
		if !exists || node.Type != "recommend" || seenSections[section.RecommendNodeID] || section.Title != node.Name || section.RenderType != node.RenderType || section.SortOrder != i+1 || section.Type != node.RenderType {
			issues = append(issues, issue("invalid_display", location, "板块必须按数组顺序，保留节点原名和 renderType，且引用唯一。"))
		}
		seenSections[section.RecommendNodeID] = true
		if section.VisibleWhen != nil {
			issues = append(issues, checkCondition(*section.VisibleWhen, fields, location+".visibleWhen", 0)...)
		}
		if node.RenderType == "batchRecommend" && section.LookupTableRef != "lookup_"+section.RecommendNodeID {
			issues = append(issues, issue("invalid_lookup_ref", location, "lookupTableRef 必须引用当前推荐节点的 lookup_<nodeId>。"))
		}
	}
	for id, node := range spec.Recommendation.DecisionTree.Nodes {
		if node.Type == "recommend" && !seenSections[id] {
			issues = append(issues, issue("missing_section", "display.sections", "推荐节点 "+id+" 没有展示板块。"))
		}
	}
	return issues
}

func checkCondition(c Condition, fields map[string]Field, path string, depth int) []Issue {
	if depth > 32 {
		return []Issue{issue("invalid_condition", path, "条件嵌套过深。")}
	}
	if c.Op == "and" || c.Op == "or" {
		if len(c.Children) == 0 || c.Child != nil || c.Field != "" || len(c.Value) > 0 {
			return []Issue{issue("invalid_condition", path, "and/or 仅使用非空 children。")}
		}
		var issues []Issue
		for i, child := range c.Children {
			issues = append(issues, checkCondition(child, fields, fmt.Sprintf("%s.children[%d]", path, i), depth+1)...)
		}
		return issues
	}
	if c.Op == "not" {
		if c.Child == nil || len(c.Children) > 0 || c.Field != "" || len(c.Value) > 0 {
			return []Issue{issue("invalid_condition", path, "not 仅使用 child。")}
		}
		return checkCondition(*c.Child, fields, path+".child", depth+1)
	}
	field, ok := fields[c.Field]
	if !ok {
		return []Issue{issue("unknown_field", path, "条件引用了未知字段。")}
	}
	var value any
	if json.Unmarshal(c.Value, &value) != nil || !literalValue(value) {
		return []Issue{issue("invalid_condition", path, "条件需要字面量，不能传任意对象。")}
	}
	sample := json.RawMessage(`"sample"`)
	if field.Type == "number" {
		sample = json.RawMessage("0")
	}
	if _, err := EvaluateCondition(c, map[string]Literal{c.Field: sample}); err != nil {
		return []Issue{issue("invalid_condition", path, err.Error())}
	}
	return nil
}

func checkFormulaCoverage(formulas []Formula, fields map[string]Field, snapshot Snapshot) []Issue {
	if len(formulas) <= 1 && (len(formulas) == 0 || len(formulas[0].BranchSpec) == 0) {
		return nil
	}
	keys := make(map[string]bool)
	for _, formula := range formulas {
		for key := range formula.BranchSpec {
			keys[key] = true
		}
	}
	if len(keys) == 0 {
		return []Issue{issue("ambiguous_formula", "scoring.scoreCalc", "多公式必须按完整分支组合区分。")}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	combinations := []map[string]string{{}}
	for _, key := range ordered {
		field, exists := fields[key]
		if !exists {
			return []Issue{issue("unknown_branch_field", "scoring.scoreCalc.branchSpec", "未知分支字段 "+key)}
		}
		values := fieldOptions(field, snapshot)
		if len(values) == 0 {
			return []Issue{issue("unknown_branch_values", "scoring.scoreCalc.branchSpec", "分支必须使用可枚举的真实取值。")}
		}
		if len(combinations)*len(values) > 4096 {
			return []Issue{issue("unsupported_rule", "scoring.scoreCalc.branchSpec", "分支组合超过当前检查预算。")}
		}
		var next []map[string]string
		for _, combo := range combinations {
			for _, value := range values {
				copy := make(map[string]string)
				for k, v := range combo {
					copy[k] = v
				}
				copy[key] = value
				next = append(next, copy)
			}
		}
		combinations = next
		for _, formula := range formulas {
			for _, value := range formula.BranchSpec[key] {
				if !oneOf(value, values...) {
					return []Issue{issue("invalid_branch_value", "scoring.scoreCalc.branchSpec", "分支使用了不存在的取值。")}
				}
			}
		}
	}
	for _, combo := range combinations {
		matches := 0
		for _, formula := range formulas {
			if formulaMatches(formula, combo) {
				matches++
			}
		}
		if matches != 1 {
			return []Issue{issue("formula_coverage", "scoring.scoreCalc.branchSpec", fmt.Sprintf("组合 %v 匹配 %d 条公式，必须恰好一条。", combo, matches))}
		}
	}
	return nil
}

func fieldOptions(field Field, snapshot Snapshot) []string {
	if field.DataRef != "" {
		var values []string
		for _, option := range snapshot.Catalog[field.DataRef] {
			values = append(values, option.ID)
		}
		return values
	}
	if len(field.OptionsEnum) > 0 {
		var values []string
		for _, option := range field.OptionsEnum {
			values = append(values, option.ID)
		}
		return values
	}
	return field.Options
}

func formulaMatches(formula Formula, fields map[string]string) bool {
	for key, values := range formula.BranchSpec {
		if !oneOf(fields[key], values...) {
			return false
		}
	}
	return true
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

// OneOf is shared with the repository's persisted status validation.
func OneOf(value string, options ...string) bool { return oneOf(value, options...) }
func finite(value float64) bool                  { return !math.IsNaN(value) && !math.IsInf(value, 0) }
