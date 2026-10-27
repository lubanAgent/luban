package strategyagent

import (
	"fmt"
	"math"
)

const maxLookupComparisons = 5_000_000

func compileLookups(spec StrategySpec, snapshot Snapshot) ([]LookupTable, error) {
	minScore, maxScore := math.Inf(1), math.Inf(-1)
	fields := make(map[string]Field)
	for _, field := range spec.Scoring.ScoreInput.Fields {
		fields[field.Key] = field
	}
	for _, formula := range spec.Scoring.ScoreCalc {
		low, high := formula.Offset, formula.Offset
		for _, input := range formula.Inputs {
			field := fields[input.Field]
			weight := 1.0
			if input.Weight != nil {
				weight = *input.Weight
			}
			a, b := (*field.Min+input.Offset)*weight, (*field.Max+input.Offset)*weight
			low += math.Min(a, b)
			high += math.Max(a, b)
		}
		minScore = math.Min(minScore, low)
		maxScore = math.Max(maxScore, high)
	}
	if !finite(minScore) || !finite(maxScore) || maxScore-minScore > 10000 {
		return nil, fmt.Errorf("unsupported_lookup_domain: score domain exceeds 10001 integer keys")
	}
	keyCount := int64(math.Floor(maxScore) - math.Ceil(minScore) + 1)
	for _, path := range spec.Recommendation.Paths {
		work := keyCount * int64(len(snapshot.Schools)) * int64(max(1, len(path.Tiers)))
		if work > maxLookupComparisons {
			return nil, fmt.Errorf("unsupported_lookup_domain: compiling %d score keys across %d schools and %d tiers exceeds the current work budget", keyCount, len(snapshot.Schools), len(path.Tiers))
		}
	}
	var lookups []LookupTable
	for _, path := range spec.Recommendation.Paths {
		node := spec.Recommendation.DecisionTree.Nodes[path.RecommendNodeID]
		if node.RenderType == "quota" {
			continue
		}
		table := LookupTable{Key: "lookup_" + path.RecommendNodeID, Entries: []LookupEntry{}}
		for key := int(math.Ceil(minScore)); key <= int(math.Floor(maxScore)); key++ {
			tiers, err := Recommend(path, snapshot, float64(key))
			if err != nil {
				return nil, err
			}
			table.Entries = append(table.Entries, LookupEntry{Key: key, Tiers: tiers})
		}
		lookups = append(lookups, table)
	}
	return lookups, nil
}
