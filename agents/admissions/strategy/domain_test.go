package strategyagent

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/lubanAgent/luban/models"
)

func number(value float64) *float64 { return &value }
func literal(value any) Literal     { raw, _ := json.Marshal(value); return raw }

func validDomain() (StrategySpec, Snapshot) {
	form := &FormSpec{}
	form.Intake.Fields = []Field{{Key: "region", Label: "地区", Type: "enum", Required: true, OptionsEnum: []EnumOption{{ID: "a", Label: "A区"}, {ID: "b", Label: "B区"}}}}
	form.Eligibility.DefaultAction = "pass"
	scoring := &ScoringSpec{ScoreCalc: []Formula{{Name: "default", Type: "weighted", Inputs: []FormulaInput{{Field: "math"}}, OutputKey: "user_score"}}}
	scoring.ScoreInput.Fields = []Field{{Key: "math", Label: "数学", Type: "number", Required: true, Min: number(0), Max: number(100)}}
	scoring.ScoreInput.MinRequired = 1
	scoring.ScoreInput.SubmitLabel = "查看结果"
	recommendation := &RecommendationSpec{DecisionTree: DecisionTree{RootNodeID: "calc", Nodes: map[string]DecisionNode{
		"calc":      {Type: "calc_score", Name: "算分", ScoreCalcRef: "default", NodeIDs: []string{"recommend"}},
		"recommend": {Type: "recommend", Name: "第三批次", RenderType: "batchRecommend"},
	}}, Paths: []RecommendationPath{{RecommendNodeID: "recommend", Mode: "match", MatchKey: "score", ScoreColumn: "score", SchoolScope: &Condition{Op: "eq", Field: "type", Value: literal("public")}, Tiers: []Tier{{Name: "冲", Range: []float64{0, 10}, Count: 2, Sort: "asc", Dedup: true}, {Name: "稳", Range: []float64{-10, 0}, Count: 2, Sort: "desc", Dedup: true}, {Name: "保", Range: []float64{-30, -10}, Count: 1, Sort: "desc", Dedup: true}}}}}
	display := &DisplaySpec{Sections: []DisplaySection{{Type: "batchRecommend", Title: "第三批次", SortOrder: 1, RecommendNodeID: "recommend", LookupTableRef: "lookup_recommend", RenderType: "batchRecommend"}}}
	snapshot := Snapshot{ID: "snapshot-1", Schools: []School{
		{ID: 1, Name: "学校A", Fields: map[string]Literal{"type": literal("public"), "score": literal(80)}},
		{ID: 2, Name: "学校B", Fields: map[string]Literal{"type": literal("public"), "score": literal(70)}},
		{ID: 3, Name: "学校C", Fields: map[string]Literal{"type": literal("public"), "score": literal(60)}},
		{ID: 4, Name: "学校D", Fields: map[string]Literal{"type": literal("public"), "score": literal(50)}},
	}, SchoolFields: map[string]string{"type": "string", "score": "number", "name": "string"}, ScoreColumns: []string{"score"}}
	return StrategySpec{Form: form, Scoring: scoring, Recommendation: recommendation, Display: display}, snapshot
}

func requirement() RequirementCard {
	return RequirementCard{Stage1Brief: "选择地区", Stage2Brief: "数学0到100分，直接按数学分算", Stage3Brief: "公办学校冲稳保", CoveredMessageSeq: 1, ExplicitRules: []SourceRule{{Text: "按数学分算", SourceMessageSeq: 1}}}
}

func TestRecommendationClosedIntervalsDedupAndCumulativeDeficit(t *testing.T) {
	spec, snapshot := validDomain()
	if issues := CheckSpec(spec, snapshot); len(issues) > 0 {
		t.Fatalf("valid spec issues: %+v", issues)
	}
	tiers, err := Recommend(spec.Recommendation.Paths[0], snapshot, 80)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiers) != 3 || tiers[0].Quota != 2 || len(tiers[0].Schools) != 1 || tiers[1].Quota != 3 || len(tiers[1].Schools) != 1 || tiers[2].Quota != 3 || len(tiers[2].Schools) != 2 {
		t.Fatalf("deficit chain: %+v", tiers)
	}
	seen := make(map[int64]bool)
	for _, tier := range tiers {
		for _, school := range tier.Schools {
			if seen[school.ID] {
				t.Fatalf("school duplicated across tiers: %d", school.ID)
			}
			seen[school.ID] = true
		}
	}
	path := spec.Recommendation.Paths[0]
	path.MatchKey = "ranking"
	path.Tiers = []Tier{{Name: "rank", Range: []float64{0, 10}, Count: 10, Sort: "asc"}}
	tiers, err = Recommend(path, snapshot, 80)
	if err != nil || len(tiers[0].Schools) != 2 || tiers[0].Schools[0].ID != 2 {
		t.Fatalf("ranking direction/order: %+v, %v", tiers, err)
	}
}

func TestFixedSchoolsPreserveOrderAndRejectUnknown(t *testing.T) {
	_, snapshot := validDomain()
	path := RecommendationPath{Mode: "fixed", Tiers: []Tier{{Name: "固定", ScoreRange: []float64{70, 80}, FixedSchools: []string{"学校B", "学校A"}}}}
	result, err := Recommend(path, snapshot, 80)
	if err != nil || len(result) != 1 || result[0].Schools[0].ID != 2 || result[0].Schools[1].ID != 1 {
		t.Fatalf("fixed result: %+v, %v", result, err)
	}
	path.Tiers[0].FixedSchools = append(path.Tiers[0].FixedSchools, "不存在的学校")
	if _, err := Recommend(path, snapshot, 75); err == nil {
		t.Fatal("unknown fixed school was ignored")
	}
}

func TestCompleteBranchCombinationAllowsSameRegionAcrossIdentity(t *testing.T) {
	spec, snapshot := validDomain()
	spec.Form.Intake.Fields = append(spec.Form.Intake.Fields, Field{Key: "identity", Label: "身份", Type: "enum", Required: true, OptionsEnum: []EnumOption{{ID: "x", Label: "X"}, {ID: "y", Label: "Y"}}})
	fields := make(map[string]Field)
	for _, field := range spec.Form.Intake.Fields {
		fields[field.Key] = field
	}
	var formulas []Formula
	for _, region := range []string{"a", "b"} {
		for _, identity := range []string{"x", "y"} {
			f := spec.Scoring.ScoreCalc[0]
			f.Name = region + identity
			f.BranchSpec = map[string][]string{"region": {region}, "identity": {identity}}
			formulas = append(formulas, f)
		}
	}
	if issues := checkFormulaCoverage(formulas, fields, snapshot); len(issues) > 0 {
		t.Fatalf("valid full combinations rejected: %+v", issues)
	}
	if issues := checkFormulaCoverage(formulas[:3], fields, snapshot); len(issues) == 0 {
		t.Fatal("missing combination passed")
	}
	if issues := checkFormulaCoverage(append(formulas, formulas[0]), fields, snapshot); len(issues) == 0 {
		t.Fatal("overlapping complete combination passed")
	}
}

func TestScopeConcurrentEditCheckAndStaleSubmission(t *testing.T) {
	spec, snapshot := validDomain()
	scope, err := NewScope("generate", 1, "private-token", 1, Draft{ID: "draft", SchemaVersion: 1, Requirements: requirement(), Spec: spec}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := scope.Check(0)
	if err != nil || !checked.Passed {
		t.Fatalf("check = %+v, %v", checked, err)
	}
	_, artifact, err := scope.Finalize(TurnResult{Outcome: "generated", DraftRevision: 0, CheckID: checked.CheckID})
	if err != nil || artifact == nil || len(artifact.DecisionTree) == 0 {
		t.Fatalf("finalize = %+v, %v", artifact, err)
	}
	// Mutating the detached artifact cannot change the stored check record.
	artifact.Spec.Form.Intake.Fields[0].Key = "changed"
	_, artifact, err = scope.Finalize(TurnResult{Outcome: "generated", DraftRevision: 0, CheckID: checked.CheckID})
	if err != nil || artifact.Spec.Form.Intake.Fields[0].Key != "region" {
		t.Fatal("artifact was not isolated")
	}
	var wg sync.WaitGroup
	results := make(chan EditResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- scope.Edit(EditDraftInput{ExpectedRevision: 0, Display: spec.Display})
		}()
	}
	wg.Wait()
	close(results)
	applied := 0
	for result := range results {
		if result.Status == "applied" {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("applied edits = %d", applied)
	}
	if _, _, err := scope.Finalize(TurnResult{Outcome: "generated", DraftRevision: 1, CheckID: checked.CheckID}); err == nil {
		t.Fatal("stale checkId accepted after edit")
	}
	if _, _, err := scope.Finalize(TurnResult{Outcome: "readyToConfirm", DraftRevision: 1}); err == nil {
		t.Fatal("generation submitted chat outcome")
	}
	raw, _ := json.Marshal(scope)
	if strings.Contains(string(raw), "private-token") {
		t.Fatal("scope leaks attempt token")
	}
}

func TestSimulationUsesFormulaAndRealSnapshot(t *testing.T) {
	spec, snapshot := validDomain()
	result, err := Simulate(spec, snapshot, StudentInput{Fields: map[string]Literal{"region": literal("a")}, Scores: map[string]float64{"math": 80}})
	if err != nil || len(result.Issues) > 0 || len(result.Sections) != 1 || result.Sections[0].Score != 80 || result.Sections[0].Formula != "default" {
		t.Fatalf("simulation: %+v, %v", result, err)
	}
	if result.Sections[0].Title != "第三批次" || result.Sections[0].Tiers[0].Schools[0].Name != "学校A" {
		t.Fatalf("wrong student result: %+v", result)
	}
	result, err = Simulate(spec, snapshot, StudentInput{Fields: map[string]Literal{"region": literal("unknown")}, Scores: map[string]float64{"math": 80}})
	if err != nil || len(result.Issues) == 0 {
		t.Fatal("unknown actual option accepted")
	}
}

func TestDecisionTreeCoversRealStudentOptions(t *testing.T) {
	spec, snapshot := validDomain()
	spec.Recommendation.DecisionTree = DecisionTree{
		RootNodeID: "region",
		Nodes: map[string]DecisionNode{
			"region":    {Type: "judge", Name: "按地区", FieldRef: "region", Branches: []Branch{{Values: []string{"a"}, Next: "calc"}}},
			"calc":      {Type: "calc_score", Name: "算分", ScoreCalcRef: "default", NodeIDs: []string{"recommend"}},
			"recommend": {Type: "recommend", Name: "第三批次", RenderType: "batchRecommend"},
		},
	}
	issues := CheckSpec(spec, snapshot)
	for _, found := range issues {
		if found.Code == "missing_branch_value" {
			return
		}
	}
	t.Fatalf("decision tree did not report uncovered region b: %+v", issues)
}

func TestSimulationUsesFormulaForTheSelectedRecommendationPath(t *testing.T) {
	spec, snapshot := validDomain()
	base := spec.Scoring.ScoreCalc[0]
	base.Name = "region_a"
	base.BranchSpec = map[string][]string{"region": {"a"}}
	other := spec.Scoring.ScoreCalc[0]
	other.Name = "region_b"
	other.Offset = 10
	other.BranchSpec = map[string][]string{"region": {"b"}}
	spec.Scoring.ScoreCalc = []Formula{base, other}
	spec.Recommendation = &RecommendationSpec{
		DecisionTree: DecisionTree{
			RootNodeID: "region",
			Nodes: map[string]DecisionNode{
				"region": {Type: "judge", Name: "按地区", FieldRef: "region", Branches: []Branch{
					{Values: []string{"a"}, Next: "calc_a"},
					{Values: []string{"b"}, Next: "calc_b"},
				}},
				"calc_a":      {Type: "calc_score", Name: "A区算分", ScoreCalcRef: "region_a", NodeIDs: []string{"recommend_a"}},
				"calc_b":      {Type: "calc_score", Name: "B区算分", ScoreCalcRef: "region_b", NodeIDs: []string{"recommend_b"}},
				"recommend_a": {Type: "recommend", Name: "A区推荐", RenderType: "batchRecommend"},
				"recommend_b": {Type: "recommend", Name: "B区推荐", RenderType: "batchRecommend"},
			},
		},
		Paths: []RecommendationPath{
			{RecommendNodeID: "recommend_a", Mode: "match", MatchKey: "score", ScoreColumn: "score", SchoolScope: &Condition{Op: "eq", Field: "type", Value: literal("public")}, Tiers: []Tier{{Name: "冲", Range: []float64{0, 10}, Count: 2, Sort: "asc"}}},
			{RecommendNodeID: "recommend_b", Mode: "match", MatchKey: "score", ScoreColumn: "score", SchoolScope: &Condition{Op: "eq", Field: "type", Value: literal("public")}, Tiers: []Tier{{Name: "稳", Range: []float64{0, 10}, Count: 2, Sort: "asc"}}},
		},
	}
	spec.Display = &DisplaySpec{Sections: []DisplaySection{
		{Type: "batchRecommend", Title: "A区推荐", SortOrder: 1, RecommendNodeID: "recommend_a", LookupTableRef: "lookup_recommend_a", RenderType: "batchRecommend"},
		{Type: "batchRecommend", Title: "B区推荐", SortOrder: 2, RecommendNodeID: "recommend_b", LookupTableRef: "lookup_recommend_b", RenderType: "batchRecommend"},
	}}
	if issues := CheckSpec(spec, snapshot); len(issues) > 0 {
		t.Fatalf("valid multi-path spec issues: %+v", issues)
	}
	result, err := Simulate(spec, snapshot, StudentInput{Fields: map[string]Literal{"region": literal("b")}, Scores: map[string]float64{"math": 80}})
	if err != nil || len(result.Issues) > 0 || len(result.Sections) != 1 {
		t.Fatalf("simulation = %+v, %v", result, err)
	}
	section := result.Sections[0]
	if section.RecommendNodeID != "recommend_b" || section.Formula != "region_b" || section.Score != 90 {
		t.Fatalf("selected path used wrong score/formula: %+v", section)
	}
}

func TestBuildSnapshotMatchesReferenceSchoolAliases(t *testing.T) {
	snapshot, err := BuildSnapshot([]models.ConfigItem{{
		ID: 31, SchoolID: 17, ConfigType: "high_school", Name: "华附", District: "东区", Status: 1,
		DataJSON: `{"category":"区属","school_tags":"公办,重点","admission_score":"700","scores":{"2025":{"huji":"690"}},"attrs":{"campus":"本部"}}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	school := snapshot.Schools[0]
	for field, want := range map[string]string{"type": `"区属"`, "category": `"区属"`, "tags": `["公办","重点"]`, "admission_score": `700`, "admissionScore_huji": `700`, "admissionScore_huji_2025": `690`, "attrs.campus": `"本部"`, "id": `31`, "schoolId": `17`} {
		if got := string(school.Fields[field]); got != want {
			t.Errorf("school field %s = %s, want %s", field, got, want)
		}
	}
	if !oneOf("admissionScore_huji_2025", snapshot.ScoreColumns...) {
		t.Fatalf("score column aliases = %v", snapshot.ScoreColumns)
	}
}

func TestUnsupportedPrecisionAndBadConditionFailClosed(t *testing.T) {
	spec, snapshot := validDomain()
	spec.Scoring.ScoreCalc[0].Inputs[0].Weight = number(0.33)
	issues := CheckSpec(spec, snapshot)
	if len(issues) == 0 || issues[0].Code != "unsupported_precision" {
		t.Fatalf("fractional weight accepted: %+v", issues)
	}
	if _, err := EvaluateCondition(Condition{Op: "unknown", Field: "region", Value: literal("a")}, map[string]Literal{"region": literal("a")}); err == nil {
		t.Fatal("unknown operator accepted")
	}
	if _, err := EvaluateCondition(Condition{Op: "gte", Field: "math", Value: literal("other_field")}, map[string]Literal{"math": literal(80)}); err == nil {
		t.Fatal("implicit field reference accepted")
	}
}
