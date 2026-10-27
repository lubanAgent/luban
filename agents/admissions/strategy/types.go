// Package strategyagent implements the admissions domain without depending on
// the Agent package or any company framework. Models never execute Go code.
package strategyagent

import "encoding/json"

// RequirementCard retains the complete three-part requirement after each turn.
type RequirementCard struct {
	Stage1Brief       string       `json:"stage1Brief"`
	Stage2Brief       string       `json:"stage2Brief"`
	Stage3Brief       string       `json:"stage3Brief"`
	OpenQuestions     []string     `json:"openQuestions"`
	ExplicitRules     []SourceRule `json:"explicitRules"`
	CoveredMessageSeq int64        `json:"coveredMessageSeq"`
}

type SourceRule struct {
	Text             string `json:"text"`
	SourceMessageSeq int64  `json:"sourceMessageSeq"`
}

// Literal is restricted to JSON scalar values or arrays of scalars. It keeps
// existing DSL values while preventing arbitrary expression/code execution.
type Literal = json.RawMessage

type Condition struct {
	Op       string      `json:"op"`
	Field    string      `json:"field,omitempty"`
	Value    Literal     `json:"value,omitempty"`
	Children []Condition `json:"children,omitempty"`
	Child    *Condition  `json:"child,omitempty"`
}

type Field struct {
	Key         string       `json:"key"`
	Label       string       `json:"label"`
	Type        string       `json:"type"`
	Required    bool         `json:"required"`
	Min         *float64     `json:"min,omitempty"`
	Max         *float64     `json:"max,omitempty"`
	MaxLength   int          `json:"maxLength,omitempty"`
	Options     []string     `json:"options,omitempty"`
	DataRef     string       `json:"dataRef,omitempty"`
	OptionsEnum []EnumOption `json:"optionsEnum,omitempty"`
}

type EnumOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type FormSpec struct {
	Intake struct {
		Fields []Field `json:"fields"`
	} `json:"intake"`
	Eligibility struct {
		Rules         []EligibilityRule `json:"rules"`
		DefaultAction string            `json:"defaultAction"`
	} `json:"eligibility"`
}

type EligibilityRule struct {
	Condition Condition `json:"condition"`
	Action    string    `json:"action"`
	Message   string    `json:"message"`
}

type ScoringSpec struct {
	ScoreInput struct {
		Fields      []Field           `json:"fields"`
		MinRequired int               `json:"minRequired"`
		SubmitLabel string            `json:"submitLabel"`
		Validation  []ScoreValidation `json:"validation"`
	} `json:"scoreInput"`
	ScoreCalc []Formula `json:"scoreCalc"`
}

type ScoreValidation struct {
	Condition Condition `json:"condition"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
}

type Formula struct {
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Inputs     []FormulaInput      `json:"inputs"`
	Offset     float64             `json:"offset"`
	OutputKey  string              `json:"outputKey"`
	BranchSpec map[string][]string `json:"branchSpec,omitempty"`
}

type FormulaInput struct {
	Field  string   `json:"field"`
	Weight *float64 `json:"weight,omitempty"`
	Offset float64  `json:"offset"`
}

type RecommendationSpec struct {
	DecisionTree DecisionTree         `json:"decisionTree"`
	Paths        []RecommendationPath `json:"paths"`
}

type DecisionTree struct {
	RootNodeID string                  `json:"rootNodeId"`
	Nodes      map[string]DecisionNode `json:"nodes"`
}

type DecisionNode struct {
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	FieldRef     string   `json:"fieldRef,omitempty"`
	Branches     []Branch `json:"branches,omitempty"`
	ScoreCalcRef string   `json:"scoreCalcRef,omitempty"`
	NodeIDs      []string `json:"nodeIds,omitempty"`
	RenderType   string   `json:"renderType,omitempty"`
	Href         string   `json:"href,omitempty"`
}

type Branch struct {
	Label   string   `json:"label"`
	Values  []string `json:"values"`
	Next    string   `json:"next,omitempty"`
	NodeIDs []string `json:"nodeIds,omitempty"`
}

type RecommendationPath struct {
	RecommendNodeID string     `json:"recommendNodeId"`
	Mode            string     `json:"mode"`
	MatchKey        string     `json:"matchKey,omitempty"`
	ScoreColumn     string     `json:"scoreColumn,omitempty"`
	SchoolScope     *Condition `json:"schoolScope,omitempty"`
	Tiers           []Tier     `json:"tiers"`
}

type Tier struct {
	Name         string    `json:"name"`
	Range        []float64 `json:"range,omitempty"`
	Count        int       `json:"count,omitempty"`
	Sort         string    `json:"sort,omitempty"`
	Dedup        bool      `json:"dedup,omitempty"`
	ScoreRange   []float64 `json:"scoreRange,omitempty"`
	FixedSchools []string  `json:"fixedSchools,omitempty"`
}

type DisplaySpec struct {
	Sections []DisplaySection `json:"sections"`
}

type DisplaySection struct {
	Type            string     `json:"type"`
	Title           string     `json:"title"`
	SortOrder       int        `json:"sortOrder"`
	RecommendNodeID string     `json:"recommendNodeId"`
	LookupTableRef  string     `json:"lookupTableRef,omitempty"`
	RenderType      string     `json:"renderType"`
	VisibleWhen     *Condition `json:"visibleWhen,omitempty"`
}

type StrategySpec struct {
	Form           *FormSpec           `json:"form,omitempty"`
	Scoring        *ScoringSpec        `json:"scoring,omitempty"`
	Recommendation *RecommendationSpec `json:"recommendation,omitempty"`
	Display        *DisplaySpec        `json:"display,omitempty"`
}

type Draft struct {
	ID            string          `json:"draftId"`
	Revision      int64           `json:"revision"`
	SchemaVersion int             `json:"schemaVersion"`
	Requirements  RequirementCard `json:"requirements"`
	Spec          StrategySpec    `json:"spec"`
}

type School struct {
	ID     int64              `json:"schoolId"`
	Name   string             `json:"name"`
	Fields map[string]Literal `json:"fields"`
}

type Snapshot struct {
	ID           string                  `json:"snapshotId"`
	Hash         string                  `json:"hash"`
	Schools      []School                `json:"schools"`
	Catalog      map[string][]EnumOption `json:"catalog"`
	SchoolFields map[string]string       `json:"schoolFields"`
	ScoreColumns []string                `json:"scoreColumns"`
}

type Issue struct {
	Code       string `json:"code"`
	Path       string `json:"path"`
	Message    string `json:"message"`
	Repairable bool   `json:"repairable"`
}

type EditDraftInput struct {
	ExpectedRevision int64               `json:"expectedRevision"`
	Requirements     *RequirementCard    `json:"requirements,omitempty"`
	Form             *FormSpec           `json:"form,omitempty"`
	Scoring          *ScoringSpec        `json:"scoring,omitempty"`
	Recommendation   *RecommendationSpec `json:"recommendation,omitempty"`
	Display          *DisplaySpec        `json:"display,omitempty"`
}

type EditResult struct {
	Status   string  `json:"status"`
	Revision int64   `json:"revision"`
	Issues   []Issue `json:"issues"`
}

type CheckResult struct {
	Passed       bool    `json:"passed"`
	Revision     int64   `json:"revision"`
	CheckID      string  `json:"checkId,omitempty"`
	Issues       []Issue `json:"issues"`
	ArtifactHash string  `json:"artifactHash,omitempty"`
}

type TurnResult struct {
	Outcome       string `json:"outcome"`
	Reply         string `json:"reply"`
	DraftRevision int64  `json:"draftRevision"`
	CheckID       string `json:"checkId"`
}

type StudentInput struct {
	ExpectedRevision int64              `json:"expectedRevision"`
	Fields           map[string]Literal `json:"fields"`
	Scores           map[string]float64 `json:"scores"`
}

type Simulation struct {
	Eligible     bool               `json:"eligible"`
	Action       string             `json:"action,omitempty"`
	Message      string             `json:"message,omitempty"`
	VisitedNodes []string           `json:"visitedNodes"`
	Sections     []SimulatedSection `json:"sections"`
	Issues       []Issue            `json:"issues"`
}

type SimulatedSection struct {
	Title           string       `json:"title"`
	RenderType      string       `json:"renderType"`
	RecommendNodeID string       `json:"recommendNodeId"`
	Score           float64      `json:"score"`
	Formula         string       `json:"formula"`
	Tiers           []TierResult `json:"tiers"`
}

type TierResult struct {
	Name    string            `json:"name"`
	Quota   int               `json:"quota"`
	Schools []SchoolReference `json:"schools"`
}

type SchoolReference struct {
	ID   int64  `json:"schoolId"`
	Name string `json:"name"`
}

// Artifact is immutable after checking. It contains six original DSL sections;
// school results are computed from the bound snapshot, never model arithmetic.
type Artifact struct {
	Intake        json.RawMessage `json:"intake"`
	Eligibility   json.RawMessage `json:"eligibility"`
	ScoreInput    json.RawMessage `json:"scoreInput"`
	ScoreCalc     json.RawMessage `json:"scoreCalc"`
	DecisionTree  json.RawMessage `json:"decisionTree"`
	ReportDisplay json.RawMessage `json:"reportDisplay"`
	SnapshotHash  string          `json:"snapshotHash"`
	Spec          StrategySpec    `json:"spec"`
	Lookups       []LookupTable   `json:"lookups"`
}

type LookupTable struct {
	Key     string        `json:"tableKey"`
	Entries []LookupEntry `json:"entries"`
}

type LookupEntry struct {
	Key   int          `json:"key"`
	Tiers []TierResult `json:"tiers"`
}
