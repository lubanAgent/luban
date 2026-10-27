package admissions

import luban "github.com/lubanAgent/luban/sdk"

func init() {
	luban.RegisterFuncs(PrepareAdmission, FinishAdmission)
	luban.RegisterTools(QueryBusinessData{}, EditStrategyDraft{}, CheckStrategyDraft{}, SimulateStudent{})
}
