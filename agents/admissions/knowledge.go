package admissions

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

func taskKnowledge(ctx *gin.Context, kind string) (string, error) {
	names := []string{"requirementClarify", "strategyCheck"}
	if kind == "generate" {
		names = []string{"requirementClarify", "formAndEligibility", "scoreCalculation", "schoolRecommendation", "resultDisplay", "strategyCheck"}
	}
	var content strings.Builder
	for _, name := range names {
		skill, err := luban.LookupSkill(ctx, name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&content, "\n<domain-instructions name=%q>\n%s\n</domain-instructions>\n", name, skill.Content)
	}
	return content.String(), nil
}
