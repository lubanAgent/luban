package strategyagent

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Recommend computes the original closed-interval recommendation rules. The
// checked path is authoritative; model output never overrides its tier bounds.
func Recommend(path RecommendationPath, snapshot Snapshot, score float64) ([]TierResult, error) {
	if issues := checkRecommendationPath(path, snapshot, "path"); len(issues) > 0 {
		return nil, fmt.Errorf("invalid recommendation path: %s", issues[0].Message)
	}
	results := make([]TierResult, 0, len(path.Tiers))
	if path.Mode == "fixed" {
		for _, tier := range path.Tiers {
			if score < tier.ScoreRange[0] || score > tier.ScoreRange[1] {
				continue
			}
			result := TierResult{Name: tier.Name, Quota: len(tier.FixedSchools), Schools: []SchoolReference{}}
			for _, name := range tier.FixedSchools {
				for _, school := range snapshot.Schools {
					if school.Name == name {
						result.Schools = append(result.Schools, SchoolReference{ID: school.ID, Name: school.Name})
					}
				}
			}
			results = append(results, result)
		}
		return results, nil
	}
	type candidate struct {
		school School
		metric float64
	}
	var pool []candidate
	for _, school := range snapshot.Schools {
		fields := make(map[string]Literal, len(school.Fields)+1)
		for key, value := range school.Fields {
			fields[key] = value
		}
		name, _ := json.Marshal(school.Name)
		fields["name"] = name
		included, err := EvaluateCondition(*path.SchoolScope, fields)
		if err != nil {
			return nil, err
		}
		if !included {
			continue
		}
		var metric float64
		if json.Unmarshal(school.Fields[path.ScoreColumn], &metric) != nil || !finite(metric) {
			return nil, fmt.Errorf("school %d has no numeric %s", school.ID, path.ScoreColumn)
		}
		pool = append(pool, candidate{school: school, metric: metric})
	}
	used := make(map[int64]bool)
	deficit := 0
	for _, tier := range path.Tiers {
		quota := tier.Count + deficit
		var matching []candidate
		for _, c := range pool {
			if tier.Dedup && used[c.school.ID] {
				continue
			}
			diff := c.metric - score
			if path.MatchKey == "ranking" {
				diff = score - c.metric
			}
			if diff >= tier.Range[0] && diff <= tier.Range[1] {
				matching = append(matching, c)
			}
		}
		sort.SliceStable(matching, func(i, j int) bool {
			if matching[i].metric == matching[j].metric {
				return matching[i].school.ID < matching[j].school.ID
			}
			if tier.Sort == "asc" {
				return matching[i].metric < matching[j].metric
			}
			return matching[i].metric > matching[j].metric
		})
		result := TierResult{Name: tier.Name, Quota: quota, Schools: []SchoolReference{}}
		for i, c := range matching {
			if i >= quota {
				break
			}
			result.Schools = append(result.Schools, SchoolReference{ID: c.school.ID, Name: c.school.Name})
			used[c.school.ID] = true
		}
		deficit = quota - len(result.Schools)
		results = append(results, result)
	}
	return results, nil
}
