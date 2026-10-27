package strategyagent

import (
	"encoding/json"
	"fmt"
	"math"
)

// Simulate uses only a checked draft. It never creates student submissions.
func (s *Scope) Simulate(input StudentInput) (Simulation, error) {
	s.mu.Lock()
	if input.ExpectedRevision != s.draft.Revision {
		s.mu.Unlock()
		return Simulation{Issues: []Issue{issue("revision_conflict", "expectedRevision", "草稿已改变。")}}, nil
	}
	checked := false
	for _, record := range s.checks {
		if record.revision == s.draft.Revision && record.artifact != nil {
			checked = true
		}
	}
	if !checked {
		s.mu.Unlock()
		return Simulation{Issues: []Issue{issue("unchecked_spec", "spec", "请先检查当前草稿。")}}, nil
	}
	spec, _ := cloneJSON(s.draft.Spec)
	snapshot := s.snapshot
	s.mu.Unlock()
	return Simulate(spec, snapshot, input)
}

// Simulate is a candidate backend interpreter. Equivalence with the student
// page must be verified separately against its real consumer implementation.
func Simulate(spec StrategySpec, snapshot Snapshot, input StudentInput) (Simulation, error) {
	result := Simulation{Eligible: true, VisitedNodes: []string{}, Sections: []SimulatedSection{}, Issues: []Issue{}}
	if issues := CheckSpec(spec, snapshot); len(issues) > 0 {
		result.Issues = issues
		return result, nil
	}
	for _, field := range spec.Form.Intake.Fields {
		raw, exists := input.Fields[field.Key]
		if !exists || string(raw) == "null" {
			if field.Required {
				result.Issues = append(result.Issues, issue("required_field", "fields."+field.Key, "缺少必填字段。"))
			}
			continue
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			result.Issues = append(result.Issues, issue("invalid_field", "fields."+field.Key, "字段不是合法 JSON。"))
			continue
		}
		if field.Type == "number" {
			number, ok := value.(float64)
			if !ok || !finite(number) || number < *field.Min || number > *field.Max {
				result.Issues = append(result.Issues, issue("invalid_field", "fields."+field.Key, "数值不在字段范围内。"))
			}
		} else {
			text, ok := value.(string)
			if !ok || (field.MaxLength > 0 && len([]rune(text)) > field.MaxLength) {
				result.Issues = append(result.Issues, issue("invalid_field", "fields."+field.Key, "字段类型或长度不正确。"))
				continue
			}
			if options := fieldOptions(field, snapshot); len(options) > 0 && !oneOf(text, options...) {
				result.Issues = append(result.Issues, issue("invalid_option", "fields."+field.Key, "必须使用真实选项值。"))
			}
		}
	}
	for _, field := range spec.Scoring.ScoreInput.Fields {
		value, exists := input.Scores[field.Key]
		if !exists {
			if field.Required {
				result.Issues = append(result.Issues, issue("required_score", "scores."+field.Key, "缺少必填成绩。"))
			}
			continue
		}
		if !finite(value) || value < *field.Min || value > *field.Max {
			result.Issues = append(result.Issues, issue("invalid_score", "scores."+field.Key, "成绩不在允许范围。"))
		}
	}
	if len(result.Issues) > 0 {
		return result, nil
	}
	for _, rule := range spec.Form.Eligibility.Rules {
		matches, err := EvaluateCondition(rule.Condition, input.Fields)
		if err != nil {
			return Simulation{}, err
		}
		if !matches {
			continue
		}
		if rule.Action != "pass" {
			result.Eligible = false
			result.Action, result.Message = rule.Action, rule.Message
			return result, nil
		}
		break
	}
	if spec.Form.Eligibility.DefaultAction == "block" {
		passed := false
		for _, rule := range spec.Form.Eligibility.Rules {
			matches, _ := EvaluateCondition(rule.Condition, input.Fields)
			if matches {
				passed = rule.Action == "pass"
				break
			}
		}
		if !passed {
			result.Eligible = false
			result.Action = "block"
			return result, nil
		}
	}
	var walk func(string, string, float64, int) error
	formulaByNode := make(map[string]string)
	scoreByNode := make(map[string]float64)
	walk = func(id, currentFormula string, currentScore float64, depth int) error {
		if depth > 128 {
			return fmt.Errorf("decision path exceeds maximum depth")
		}
		node := spec.Recommendation.DecisionTree.Nodes[id]
		result.VisitedNodes = append(result.VisitedNodes, id)
		switch node.Type {
		case "judge":
			var value string
			if json.Unmarshal(input.Fields[node.FieldRef], &value) != nil {
				return fmt.Errorf("judge %s requires a string option", id)
			}
			for _, branch := range node.Branches {
				if !oneOf(value, branch.Values...) {
					continue
				}
				if branch.Next != "" {
					return walk(branch.Next, currentFormula, currentScore, depth+1)
				}
				for _, next := range branch.NodeIDs {
					if err := walk(next, currentFormula, currentScore, depth+1); err != nil {
						return err
					}
				}
				return nil
			}
			return fmt.Errorf("judge %s has no branch for current option", id)
		case "calc_score":
			currentFormula = node.ScoreCalcRef
			var selected *Formula
			for i := range spec.Scoring.ScoreCalc {
				if spec.Scoring.ScoreCalc[i].Name == currentFormula {
					selected = &spec.Scoring.ScoreCalc[i]
				}
			}
			if selected == nil {
				return fmt.Errorf("missing formula %s", currentFormula)
			}
			values := make(map[string]string)
			for key, raw := range input.Fields {
				var value string
				if json.Unmarshal(raw, &value) == nil {
					values[key] = value
				}
			}
			if !formulaMatches(*selected, values) {
				return fmt.Errorf("decision path formula does not match student's complete branch combination")
			}
			score := selected.Offset
			for _, part := range selected.Inputs {
				value, exists := input.Scores[part.Field]
				if !exists {
					return fmt.Errorf("formula is missing score %s", part.Field)
				}
				weight := 1.0
				if part.Weight != nil {
					weight = *part.Weight
				}
				score += (value + part.Offset) * weight
			}
			if !finite(score) || math.Trunc(score) != score {
				return fmt.Errorf("unsupported_precision: noninteger lookup score")
			}
			currentScore = score
			for _, next := range node.NodeIDs {
				if err := walk(next, currentFormula, currentScore, depth+1); err != nil {
					return err
				}
			}
		case "recommend":
			formulaByNode[id] = currentFormula
			scoreByNode[id] = currentScore
		case "endpoint":
			result.Action = "redirect"
			result.Message = node.Href
		default:
			return fmt.Errorf("unknown node type")
		}
		return nil
	}
	if err := walk(spec.Recommendation.DecisionTree.RootNodeID, "", 0, 0); err != nil {
		result.Issues = append(result.Issues, issue("student_path", "recommendation", err.Error()))
		return result, nil
	}
	for _, section := range spec.Display.Sections {
		if _, visited := formulaByNode[section.RecommendNodeID]; !visited {
			continue
		}
		if section.VisibleWhen != nil {
			visible, err := EvaluateCondition(*section.VisibleWhen, input.Fields)
			if err != nil {
				return Simulation{}, err
			}
			if !visible {
				continue
			}
		}
		shown := SimulatedSection{
			Title: section.Title, RenderType: section.RenderType, RecommendNodeID: section.RecommendNodeID,
			Score: scoreByNode[section.RecommendNodeID], Formula: formulaByNode[section.RecommendNodeID],
			Tiers: []TierResult{},
		}
		if section.RenderType == "batchRecommend" {
			for _, path := range spec.Recommendation.Paths {
				if path.RecommendNodeID == section.RecommendNodeID {
					tiers, err := Recommend(path, snapshot, shown.Score)
					if err != nil {
						return Simulation{}, err
					}
					shown.Tiers = tiers
				}
			}
		}
		result.Sections = append(result.Sections, shown)
	}
	return result, nil
}
