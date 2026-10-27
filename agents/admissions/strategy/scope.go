package strategyagent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// Scope is one task attempt's locked workspace. Nothing is persisted until a
// business Finish transaction commits; failed attempts discard this workspace.
type Scope struct {
	mu            sync.Mutex
	kind          string
	taskID        int64
	attemptToken  string
	maxMessageSeq int64
	draft         Draft
	snapshot      Snapshot
	checks        map[string]checkRecord
}

type checkRecord struct {
	revision     int64
	draftHash    string
	snapshotHash string
	artifact     *Artifact
}

// NewScope binds immutable task identity and a data snapshot to the draft.
func NewScope(kind string, taskID int64, attemptToken string, maxMessageSeq int64, draft Draft, snapshot Snapshot) (*Scope, error) {
	if !oneOf(kind, "chat", "generate") || taskID <= 0 || attemptToken == "" {
		return nil, fmt.Errorf("invalid claimed task identity")
	}
	if draft.ID == "" || draft.Revision < 0 {
		return nil, fmt.Errorf("invalid draft identity")
	}
	if snapshot.ID == "" {
		return nil, fmt.Errorf("missing business snapshot")
	}
	copyDraft, err := cloneJSON(draft)
	if err != nil {
		return nil, err
	}
	copySnapshot, err := cloneJSON(snapshot)
	if err != nil {
		return nil, err
	}
	copySnapshot.Hash = ""
	actualHash, err := hashJSON(copySnapshot)
	if err != nil {
		return nil, err
	}
	if snapshot.Hash != "" && snapshot.Hash != actualHash {
		return nil, fmt.Errorf("business snapshot hash mismatch")
	}
	copySnapshot.Hash = actualHash
	return &Scope{kind: kind, taskID: taskID, attemptToken: attemptToken, maxMessageSeq: maxMessageSeq, draft: copyDraft, snapshot: copySnapshot, checks: make(map[string]checkRecord)}, nil
}

// Read returns a detached draft so callers cannot mutate checked memory.
func (s *Scope) Read() Draft {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy, _ := cloneJSON(s.draft)
	return copy
}

// Edit compares revision and replaces exactly one complete domain section.
func (s *Scope) Edit(input EditDraftInput) EditResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := EditResult{Revision: s.draft.Revision, Issues: []Issue{}}
	if input.ExpectedRevision != s.draft.Revision {
		result.Status = "conflict"
		return result
	}
	count := 0
	for _, present := range []bool{input.Requirements != nil, input.Form != nil, input.Scoring != nil, input.Recommendation != nil, input.Display != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		result.Status = "invalid"
		result.Issues = append(result.Issues, issue("invalid_edit", "edit", "必须恰好提交一个完整部分。"))
		return result
	}
	if (s.kind == "chat" && input.Requirements == nil) || (s.kind == "generate" && input.Requirements != nil) {
		result.Status = "invalid"
		result.Issues = append(result.Issues, issue("forbidden_edit", "edit", "聊天只能修改需求；生成不能修改已确认需求。"))
		return result
	}
	copy, err := cloneJSON(input)
	if err != nil {
		result.Status = "invalid"
		result.Issues = append(result.Issues, issue("invalid_json", "edit", "编辑数据无法编码。"))
		return result
	}
	if copy.Requirements != nil {
		s.draft.Requirements = *copy.Requirements
	}
	if copy.Form != nil {
		s.draft.Spec.Form = copy.Form
	}
	if copy.Scoring != nil {
		s.draft.Spec.Scoring = copy.Scoring
	}
	if copy.Recommendation != nil {
		s.draft.Spec.Recommendation = copy.Recommendation
	}
	if copy.Display != nil {
		s.draft.Spec.Display = copy.Display
	}
	s.draft.Revision++
	s.checks = make(map[string]checkRecord)
	result.Status, result.Revision = "applied", s.draft.Revision
	return result
}

// Check signs only the exact revision that was checked. An intervening edit
// invalidates the result and cannot accidentally submit a newer draft.
func (s *Scope) Check(expectedRevision int64) (CheckResult, error) {
	s.mu.Lock()
	if expectedRevision != s.draft.Revision {
		result := CheckResult{Revision: s.draft.Revision, Issues: []Issue{issue("revision_conflict", "expectedRevision", "草稿已改变，请重新读取。")}}
		s.mu.Unlock()
		return result, nil
	}
	draft, _ := cloneJSON(s.draft)
	snapshot := s.snapshot
	s.mu.Unlock()
	issues := CheckRequirements(draft.Requirements, s.maxMessageSeq)
	var artifact *Artifact
	if s.kind == "generate" {
		issues = append(issues, CheckSpec(draft.Spec, snapshot)...)
		if len(issues) == 0 {
			built, err := compileArtifact(draft.Spec, snapshot)
			if err != nil {
				issues = append(issues, issue("unsupported_compile", "spec", err.Error()))
			} else {
				artifact = &built
			}
		}
	}
	result := CheckResult{Revision: draft.Revision, Issues: issues}
	if len(issues) > 0 {
		return result, nil
	}
	hash, err := hashJSON(draft)
	if err != nil {
		return CheckResult{}, err
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return CheckResult{}, err
	}
	id := "check_" + hex.EncodeToString(bytes)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft.Revision != draft.Revision {
		result.Issues = []Issue{issue("revision_conflict", "expectedRevision", "检查期间草稿发生改变。")}
		return result, nil
	}
	s.checks[id] = checkRecord{revision: draft.Revision, draftHash: hash, snapshotHash: snapshot.Hash, artifact: artifact}
	result.Passed, result.CheckID = true, id
	if artifact != nil {
		result.ArtifactHash, _ = hashJSON(artifact)
	}
	return result, nil
}

// Finalize validates model-submitted references. Persistence and lease CAS
// still belong to the caller's single business transaction.
func (s *Scope) Finalize(result TurnResult) (Draft, *Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (s.kind == "chat" && !oneOf(result.Outcome, "reply", "readyToConfirm")) || (s.kind == "generate" && !oneOf(result.Outcome, "generated", "needsInput")) {
		return Draft{}, nil, fmt.Errorf("invalid_final_submission: outcome is not permitted for task kind")
	}
	if result.DraftRevision != s.draft.Revision {
		return Draft{}, nil, fmt.Errorf("invalid_final_submission: stale draft revision")
	}
	var artifact *Artifact
	if result.Outcome == "readyToConfirm" || result.Outcome == "generated" {
		record, exists := s.checks[result.CheckID]
		hash, err := hashJSON(s.draft)
		if err != nil {
			return Draft{}, nil, err
		}
		if !exists || record.revision != s.draft.Revision || record.draftHash != hash || record.snapshotHash != s.snapshot.Hash || (result.Outcome == "generated" && record.artifact == nil) {
			return Draft{}, nil, fmt.Errorf("invalid_final_submission: checkId does not match current draft and snapshot")
		}
		if record.artifact != nil {
			copy, err := cloneJSON(*record.artifact)
			if err != nil {
				return Draft{}, nil, err
			}
			artifact = &copy
		}
	}
	copy, err := cloneJSON(s.draft)
	return copy, artifact, err
}

func compileArtifact(spec StrategySpec, snapshot Snapshot) (Artifact, error) {
	artifact := Artifact{SnapshotHash: snapshot.Hash, Spec: spec}
	for _, part := range []struct {
		value  any
		target *json.RawMessage
	}{
		{spec.Form.Intake, &artifact.Intake}, {spec.Form.Eligibility, &artifact.Eligibility},
		{spec.Scoring.ScoreInput, &artifact.ScoreInput}, {spec.Scoring.ScoreCalc, &artifact.ScoreCalc},
		{spec.Recommendation.DecisionTree, &artifact.DecisionTree}, {spec.Display, &artifact.ReportDisplay},
	} {
		raw, err := json.Marshal(part.value)
		if err != nil {
			return Artifact{}, err
		}
		*part.target = raw
	}
	lookups, err := compileLookups(spec, snapshot)
	if err != nil {
		return Artifact{}, err
	}
	artifact.Lookups = lookups
	return artifact, nil
}

func cloneJSON[T any](value T) (T, error) {
	var copy T
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	err = json.Unmarshal(raw, &copy)
	return copy, err
}

func hashJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// HashJSON gives persistence the same canonical JSON hash used by Agent checks.
func HashJSON(value any) (string, error) { return hashJSON(value) }
