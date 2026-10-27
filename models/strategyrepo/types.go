// Package strategyrepo persists the admissions Agent's task and result data.
// Domain rules live under agents/admissions/strategy; persistence depends on
// those rules, never the reverse.
package strategyrepo

import strategyagent "github.com/lubanAgent/luban/agents/admissions/strategy"

type ClaimedTask = strategyagent.ClaimedTask
type TaskMaterial = strategyagent.TaskMaterial
type Draft = strategyagent.Draft
type Artifact = strategyagent.Artifact
type TurnResult = strategyagent.TurnResult
type Reply = strategyagent.Reply
type Snapshot = strategyagent.Snapshot
type StrategySpec = strategyagent.StrategySpec
type TaskRepository = strategyagent.TaskRepository
type HistoryMessage = strategyagent.HistoryMessage

var ErrLeaseLost = strategyagent.ErrLeaseLost

func hashJSON(value any) (string, error) { return strategyagent.HashJSON(value) }
func oneOf(value string, options ...string) bool {
	return strategyagent.OneOf(value, options...)
}
