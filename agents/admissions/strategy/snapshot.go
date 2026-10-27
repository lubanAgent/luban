package strategyagent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lubanAgent/luban/models"
)

// BuildSnapshot converts the reference module's ConfigItems. Type and category
// remain separate; malformed data or inconsistent field types fail visibly.
func BuildSnapshot(items []models.ConfigItem) (Snapshot, error) {
	result := Snapshot{Catalog: make(map[string][]EnumOption), SchoolFields: make(map[string]string), Schools: []School{}}
	sets := make(map[string]map[string]bool)
	add := func(kind, id, label string) {
		if sets[kind] == nil {
			sets[kind] = make(map[string]bool)
		}
		if !sets[kind][id] {
			sets[kind][id] = true
			result.Catalog[kind] = append(result.Catalog[kind], EnumOption{ID: id, Label: label})
		}
	}
	for _, item := range items {
		if item.Status != 1 {
			continue
		}
		switch item.ConfigType {
		case "district":
			// The reference S1 contract sends the human-readable district
			// values into form fields and later uses the same values in
			// schoolScope. IDs remain available on the source row but are not
			// the DSL value.
			add("districts", item.Name, item.Name)
		case "junior_school":
			add("middleSchools", item.Name, item.Name)
		case "high_school":
			fields := map[string]Literal{
				"id":         mustJSON(item.ID),
				"schoolId":   mustJSON(item.SchoolID),
				"configType": mustJSON(item.ConfigType),
				"name":       mustJSON(item.Name),
				"district":   mustJSON(item.District),
			}
			if item.DataJSON != "" && json.Unmarshal([]byte(item.DataJSON), &fields) != nil {
				return Snapshot{}, fmt.Errorf("invalid high_school ConfigItem %d data_json", item.ID)
			}
			if category := fields["category"]; len(category) > 0 {
				fields["type"] = category
			}
			if item.District == "省市属" {
				fields["category"] = mustJSON("市属")
				if len(fields["type"]) == 0 {
					fields["type"] = mustJSON("市属")
				}
			} else if strings.TrimSpace(item.District) != "" && len(fields["category"]) == 0 {
				fields["category"] = mustJSON("区属")
			}
			var attributes map[string]Literal
			if raw := fields["attrs"]; len(raw) > 0 {
				if json.Unmarshal(raw, &attributes) != nil {
					return Snapshot{}, fmt.Errorf("invalid school %d attrs", item.ID)
				}
				for key, value := range attributes {
					fields[key] = value
					fields["attrs."+key] = value
				}
			}
			// Preserve the aliases used by the existing filter/lookup consumers.
			if raw := fields["admission_score"]; len(raw) > 0 {
				fields["admissionScore"], fields["admissionScore_huji"], fields["admissionScore_feihuji"] = raw, raw, raw
			}
			if raw := fields["scores"]; len(raw) > 0 {
				var years map[string]map[string]Literal
				if json.Unmarshal(raw, &years) != nil {
					return Snapshot{}, fmt.Errorf("invalid school %d scores", item.ID)
				}
				for year, groups := range years {
					for group, value := range groups {
						fields["scores."+year+"."+group], fields["scores_"+year+"_"+group], fields["admissionScore_"+group+"_"+year] = value, value, value
					}
				}
			}
			if raw := fields["school_tags"]; len(raw) > 0 && len(fields["tags"]) == 0 {
				var text string
				if json.Unmarshal(raw, &text) != nil {
					return Snapshot{}, fmt.Errorf("invalid school %d school_tags", item.ID)
				}
				var tags []string
				for _, tag := range strings.Split(text, ",") {
					if tag = strings.TrimSpace(tag); tag != "" {
						tags = append(tags, tag)
					}
				}
				fields["tags"], _ = json.Marshal(tags)
				if len(fields["type"]) == 0 {
					for _, tag := range tags {
						if tag == "公办" || tag == "民办" {
							fields["type"] = mustJSON(tag)
							break
						}
					}
				}
			}
			// Legacy ConfigItem score values may be numeric JSON or numeric
			// strings. The reference lookup compares both as numbers.
			for key, raw := range fields {
				if strings.HasPrefix(key, "admissionScore") || key == "admission_score" {
					var text string
					if json.Unmarshal(raw, &text) == nil {
						if number, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
							fields[key] = mustJSON(number)
						}
					}
				}
			}
			for key, value := range fields {
				var parsed any
				if json.Unmarshal(value, &parsed) != nil {
					return Snapshot{}, fmt.Errorf("invalid school field %s", key)
				}
				kind := ""
				switch typed := parsed.(type) {
				case string:
					kind = "string"
					if oneOf(key, "type", "category", "district") {
						add(key, typed, typed)
					}
				case float64:
					kind = "number"
				case []any:
					kind = "string[]"
					for _, part := range typed {
						text, ok := part.(string)
						if !ok {
							kind = ""
							break
						}
						if key == "tags" {
							add("tags", text, text)
						}
					}
				}
				if kind == "" {
					continue
				}
				if existing := result.SchoolFields[key]; existing != "" && existing != kind {
					return Snapshot{}, fmt.Errorf("inconsistent school field type %s", key)
				}
				result.SchoolFields[key] = kind
				if (strings.HasPrefix(key, "admissionScore") || key == "admission_score") && !oneOf(key, result.ScoreColumns...) {
					result.ScoreColumns = append(result.ScoreColumns, key)
				}
			}
			result.Schools = append(result.Schools, School{ID: item.ID, Name: item.Name, Fields: fields})
		}
	}
	// The current reference module supplies this fixed grade dictionary.
	result.Catalog["grades"] = []EnumOption{{ID: "2", Label: "初一"}, {ID: "3", Label: "初二"}, {ID: "4", Label: "初三"}}
	sort.Slice(result.Schools, func(i, j int) bool { return result.Schools[i].ID < result.Schools[j].ID })
	sort.Strings(result.ScoreColumns)
	for key := range result.Catalog {
		sort.Slice(result.Catalog[key], func(i, j int) bool { return result.Catalog[key][i].ID < result.Catalog[key][j].ID })
	}
	hash, err := hashJSON(result)
	if err != nil {
		return Snapshot{}, err
	}
	result.ID = "snapshot_" + hash[:24]
	result.Hash, err = hashJSON(result)
	return result, err
}

func mustJSON(value any) Literal {
	raw, _ := json.Marshal(value)
	return raw
}
