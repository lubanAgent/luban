package admissions

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents/admissions/strategy"
	luban "github.com/lubanAgent/luban/sdk"
)

type QueryInput struct {
	Resource string `json:"resource" jsonschema:"description=catalog or schools or draft"`
	Catalog  string `json:"catalog,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type QueryResult struct {
	SnapshotID   string                                `json:"snapshotId"`
	Draft        *strategyagent.Draft                  `json:"draft,omitempty"`
	Catalog      map[string][]strategyagent.EnumOption `json:"catalog,omitempty"`
	SchoolFields map[string]string                     `json:"schoolFields,omitempty"`
	ScoreColumns []string                              `json:"scoreColumns,omitempty"`
	Schools      []strategyagent.School                `json:"schools,omitempty"`
	HasMore      bool                                  `json:"hasMore"`
	NextCursor   string                                `json:"nextCursor,omitempty"`
	Issues       []strategyagent.Issue                 `json:"issues"`
}

type QueryBusinessData struct{}

func (QueryBusinessData) Name() string { return "queryBusinessData" }
func (QueryBusinessData) Description() string {
	return "查询当前任务的真实目录、分页学校或完整工作草稿。不能传任意策略ID、SQL或URL。schools 每页最多100条，hasMore 为真时继续下一页。"
}
func (QueryBusinessData) Input() any { return QueryInput{} }
func (QueryBusinessData) Invoke(ctx *gin.Context, input any) (any, error) {
	args, ok := input.(QueryInput)
	if !ok {
		return nil, fmt.Errorf("queryBusinessData input type mismatch")
	}
	scope, err := activeScope(ctx)
	if err != nil {
		return nil, err
	}
	value, _ := luban.Get(ctx, materialKey)
	material, ok := value.(strategyagent.TaskMaterial)
	if !ok {
		return nil, fmt.Errorf("missing trusted task material")
	}
	result := QueryResult{SnapshotID: material.Snapshot.ID, Issues: []strategyagent.Issue{}}
	switch args.Resource {
	case "draft":
		draft := scope.Read()
		result.Draft = &draft
	case "catalog":
		result.Catalog = make(map[string][]strategyagent.EnumOption)
		for key, options := range material.Snapshot.Catalog {
			if args.Catalog == "" || key == args.Catalog {
				result.Catalog[key] = options
			}
		}
		result.SchoolFields, result.ScoreColumns = material.Snapshot.SchoolFields, material.Snapshot.ScoreColumns
	case "schools":
		index := 0
		if args.Cursor != "" {
			index, err = strconv.Atoi(args.Cursor)
			if err != nil || index < 0 || index > len(material.Snapshot.Schools) {
				result.Issues = append(result.Issues, strategyagent.Issue{Code: "invalid_cursor", Message: "请使用上次返回的 nextCursor。", Repairable: true})
				return result, nil
			}
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 100 {
			limit = 100
		}
		end := min(index+limit, len(material.Snapshot.Schools))
		result.Schools = material.Snapshot.Schools[index:end]
		result.HasMore = end < len(material.Snapshot.Schools)
		if result.HasMore {
			result.NextCursor = strconv.Itoa(end)
		}
	default:
		result.Issues = append(result.Issues, strategyagent.Issue{Code: "unsupported_resource", Message: "resource 必须是 catalog、schools 或 draft。", Repairable: true})
	}
	return result, nil
}

type EditStrategyDraft struct{}

func (EditStrategyDraft) Name() string { return "editStrategyDraft" }
func (EditStrategyDraft) Description() string {
	return "以 expectedRevision 乐观锁完整替换一个部分，其他部分保留。恰好提交 requirements/form/scoring/recommendation/display 之一。chat仅改requirements，generate仅改spec。修改后旧checkId失效。"
}
func (EditStrategyDraft) Input() any { return strategyagent.EditDraftInput{} }
func (EditStrategyDraft) Invoke(ctx *gin.Context, input any) (any, error) {
	args, ok := input.(strategyagent.EditDraftInput)
	if !ok {
		return nil, fmt.Errorf("editStrategyDraft input type mismatch")
	}
	scope, err := activeScope(ctx)
	if err != nil {
		return nil, err
	}
	return scope.Edit(args), nil
}

type CheckInput struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}
type CheckStrategyDraft struct{}

func (CheckStrategyDraft) Name() string { return "checkStrategyDraft" }
func (CheckStrategyDraft) Description() string {
	return "检查当前完整草稿。返回可修复问题；passed=true 后签发绑定当前revision和数据快照的checkId。提交最终结论前必须检查，不得伪造checkId。"
}
func (CheckStrategyDraft) Input() any { return CheckInput{} }
func (CheckStrategyDraft) Invoke(ctx *gin.Context, input any) (any, error) {
	args, ok := input.(CheckInput)
	if !ok {
		return nil, fmt.Errorf("checkStrategyDraft input type mismatch")
	}
	scope, err := activeScope(ctx)
	if err != nil {
		return nil, err
	}
	return scope.Check(args.ExpectedRevision)
}

type SimulateStudent struct{}

func (SimulateStudent) Name() string { return "simulateStudent" }
func (SimulateStudent) Description() string {
	return "用已检查的当前spec推演一个学生：资格、成绩范围、公式、路径、展示和真实学校。只读，不创建学生数据；需要expectedRevision。"
}
func (SimulateStudent) Input() any { return strategyagent.StudentInput{} }
func (SimulateStudent) Invoke(ctx *gin.Context, input any) (any, error) {
	args, ok := input.(strategyagent.StudentInput)
	if !ok {
		return nil, fmt.Errorf("simulateStudent input type mismatch")
	}
	scope, err := activeScope(ctx)
	if err != nil {
		return nil, err
	}
	return scope.Simulate(args)
}
