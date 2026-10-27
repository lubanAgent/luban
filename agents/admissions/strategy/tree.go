package strategyagent

import "fmt"

func checkTree(spec RecommendationSpec, fields map[string]Field, formulas map[string]Formula, snapshot Snapshot) []Issue {
	var issues []Issue
	tree := spec.DecisionTree
	if tree.RootNodeID == "" || len(tree.Nodes) == 0 {
		return []Issue{issue("missing_tree", "recommendation.decisionTree", "需要根节点和非空 nodes。")}
	}
	for id, node := range tree.Nodes {
		location := "recommendation.decisionTree.nodes." + id
		if node.Name == "" {
			issues = append(issues, issue("missing_node_name", location, "节点名称不能为空。"))
		}
		switch node.Type {
		case "judge":
			field, exists := fields[node.FieldRef]
			if !exists || len(node.Branches) == 0 {
				issues = append(issues, issue("invalid_judge", location, "judge 需要合法 fieldRef 和 branches。"))
			} else if !oneOf(field.Type, "select", "searchSelect", "enum") {
				issues = append(issues, issue("invalid_judge_field", location, "judge 只能引用具有完整真实选项的选择字段。"))
			} else if values := fieldOptions(field, snapshot); len(values) == 0 {
				issues = append(issues, issue("missing_judge_options", location, "judge 字段没有可核对的真实选项。"))
			}
			seen := make(map[string]bool)
			for _, branch := range node.Branches {
				if len(branch.Values) == 0 || (branch.Next == "") == (len(branch.NodeIDs) == 0) {
					issues = append(issues, issue("invalid_branch", location, "branch 需要 values，并且 next/nodeIds 恰好选一种。"))
				}
				for _, value := range branch.Values {
					if seen[value] {
						issues = append(issues, issue("overlapping_branch", location, "同一 judge 的取值不能重复。"))
					}
					if exists && !oneOf(value, fieldOptions(field, snapshot)...) {
						issues = append(issues, issue("unknown_branch_value", location, "judge 分支包含字段选项中不存在的值。"))
					}
					seen[value] = true
				}
			}
			if exists && oneOf(field.Type, "select", "searchSelect", "enum") {
				for _, value := range fieldOptions(field, snapshot) {
					if !seen[value] {
						issues = append(issues, issue("missing_branch_value", location, "judge 分支没有覆盖选项 "+value))
					}
				}
			}
		case "calc_score":
			if _, exists := formulas[node.ScoreCalcRef]; !exists || len(node.NodeIDs) == 0 {
				issues = append(issues, issue("invalid_formula_ref", location, "calc_score 需要合法 scoreCalcRef 和终端 nodeIds。"))
			}
		case "recommend":
			if !oneOf(node.RenderType, "batchRecommend", "quota") || len(node.NodeIDs) > 0 || len(node.Branches) > 0 {
				issues = append(issues, issue("invalid_recommend", location, "推荐节点必须是 batchRecommend/quota，且没有出边。"))
			}
		case "endpoint":
			if node.Href == "" || len(node.NodeIDs) > 0 || len(node.Branches) > 0 {
				issues = append(issues, issue("invalid_endpoint", location, "endpoint 必须提供 href，且没有出边。"))
			}
		default:
			issues = append(issues, issue("invalid_node_type", location, "未知决策节点类型。"))
		}
	}
	state := make(map[string]int)
	var walk func(string, string, int)
	walk = func(id, formula string, depth int) {
		if depth > 128 {
			issues = append(issues, issue("invalid_tree", "recommendation.decisionTree", "决策路径过深。"))
			return
		}
		node, exists := tree.Nodes[id]
		if !exists {
			issues = append(issues, issue("unknown_node", "recommendation.decisionTree", "缺少节点 "+id))
			return
		}
		if state[id] == 1 {
			issues = append(issues, issue("cyclic_tree", "recommendation.decisionTree", "决策树存在环。"))
			return
		}
		state[id] = 1
		defer func() { state[id] = 2 }()
		if node.Type == "calc_score" {
			formula = node.ScoreCalcRef
		}
		if node.Type == "recommend" && formula == "" {
			issues = append(issues, issue("missing_path_formula", "recommendation.decisionTree.nodes."+id, "推荐路径必须先经过算分节点。"))
		}
		for _, next := range node.NodeIDs {
			if target, ok := tree.Nodes[next]; ok && !oneOf(target.Type, "recommend", "endpoint") {
				issues = append(issues, issue("invalid_edge", "recommendation.decisionTree.nodes."+id, "nodeIds 只引用终端节点。"))
			}
			walk(next, formula, depth+1)
		}
		for _, branch := range node.Branches {
			if branch.Next != "" {
				if target, ok := tree.Nodes[branch.Next]; ok && !oneOf(target.Type, "judge", "calc_score") {
					issues = append(issues, issue("invalid_edge", "recommendation.decisionTree.nodes."+id, "next 只引用控制节点。"))
				}
				walk(branch.Next, formula, depth+1)
			}
			for _, next := range branch.NodeIDs {
				walk(next, formula, depth+1)
			}
		}
	}
	walk(tree.RootNodeID, "", 0)
	for id := range tree.Nodes {
		if state[id] == 0 {
			issues = append(issues, issue("unreachable_node", "recommendation.decisionTree.nodes."+id, "节点不可达。"))
		}
	}
	return issues
}

func checkRecommendationPath(p RecommendationPath, snapshot Snapshot, location string) []Issue {
	var issues []Issue
	if !oneOf(p.Mode, "match", "fixed") {
		return []Issue{issue("invalid_mode", location, "推荐模式只能是 match/fixed。")}
	}
	if p.Mode == "match" {
		if !oneOf(p.MatchKey, "score", "ranking") {
			issues = append(issues, issue("unsupported_match_key", location, "只支持 score/ranking；probability 不能自动按分数处理。"))
		}
		if !oneOf(p.ScoreColumn, snapshot.ScoreColumns...) {
			issues = append(issues, issue("invalid_score_column", location, "分数列必须来自快照白名单。"))
		}
		if p.SchoolScope == nil {
			issues = append(issues, issue("missing_school_scope", location, "必须明确学校范围。"))
		} else {
			fields := make(map[string]Field)
			for name, kind := range snapshot.SchoolFields {
				typ := "text"
				if kind == "number" {
					typ = "number"
				}
				fields[name] = Field{Key: name, Type: typ}
			}
			issues = append(issues, checkCondition(*p.SchoolScope, fields, location+".schoolScope", 0)...)
		}
		if len(p.Tiers) == 0 {
			issues = append(issues, issue("missing_tiers", location, "动态推荐需要 tiers。"))
		}
	} else if p.SchoolScope != nil || p.ScoreColumn != "" || p.MatchKey != "" {
		issues = append(issues, issue("mixed_mode", location, "固定名单不混用动态范围配置。"))
	}
	for i, tier := range p.Tiers {
		path := fmt.Sprintf("%s.tiers[%d]", location, i)
		if tier.Name == "" {
			issues = append(issues, issue("missing_tier_name", path, "保留用户原始分层名称。"))
		}
		if p.Mode == "match" {
			if len(tier.Range) != 2 || !orderedBounds(tier.Range) || tier.Count <= 0 || !oneOf(tier.Sort, "asc", "desc") || len(tier.ScoreRange) > 0 || len(tier.FixedSchools) > 0 {
				issues = append(issues, issue("invalid_match_tier", path, "动态层需要闭区间 range、正 count、asc/desc，不能含固定名单。"))
			}
		} else {
			if len(tier.ScoreRange) != 2 || !orderedBounds(tier.ScoreRange) || len(tier.Range) > 0 || tier.Count != 0 || tier.Sort != "" || tier.Dedup {
				issues = append(issues, issue("invalid_fixed_tier", path, "固定层仅使用 scoreRange 和完整 fixedSchools，不能混用动态字段。"))
			}
			for _, name := range tier.FixedSchools {
				matches := 0
				for _, school := range snapshot.Schools {
					if school.Name == name {
						matches++
					}
				}
				if matches != 1 {
					issues = append(issues, issue("unresolved_school", path, "校名 "+name+" 必须在快照中唯一存在。"))
				}
			}
		}
	}
	return issues
}

func orderedBounds(bounds []float64) bool {
	return len(bounds) == 2 && finite(bounds[0]) && finite(bounds[1]) && bounds[0] <= bounds[1]
}
