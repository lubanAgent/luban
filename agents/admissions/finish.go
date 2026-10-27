package admissions

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents/admissions/strategy"
	luban "github.com/lubanAgent/luban/sdk"
)

// FinishAdmission does not trust the model's outcome or checkId. Scope verifies
// their exact revision/hash; Commit rechecks token, lease and database version.
func FinishAdmission(ctx *gin.Context, result strategyagent.TurnResult) (strategyagent.Reply, error) {
	scope, err := activeScope(ctx)
	if err != nil {
		return strategyagent.Reply{}, err
	}
	draft, artifact, err := scope.Finalize(result)
	if err != nil {
		return strategyagent.Reply{}, err
	}
	value, ok := luban.Get(ctx, materialKey)
	material, valid := value.(strategyagent.TaskMaterial)
	if !ok || !valid {
		return strategyagent.Reply{}, fmt.Errorf("missing trusted task material")
	}
	repository, err := taskRepository(ctx)
	if err != nil {
		return strategyagent.Reply{}, err
	}
	return repository.Commit(taskContext(ctx), material, draft, artifact, result)
}
