package tracegorm

import (
	"fmt"
	"hash/crc32"
)

const runTable = "tblLubanTraceRun"
const spanShardCount = 20

// SpanTable returns the physical span table for a Run. Use this same function
// when exporting events; it performs no database access or input validation.
// The complete, case-sensitive runId is hashed with CRC32 IEEE modulo 20.
// Table names contain only a fixed prefix and an unpadded shard number, never
// caller-supplied SQL. Changing this rule requires migrating existing data.
func SpanTable(runID string) string {
	return spanTableByIndex(spanShard(runID))
}

func spanShard(runID string) int {
	return int(crc32.ChecksumIEEE([]byte(runID)) % spanShardCount)
}

func spanTableByIndex(index int) string {
	return fmt.Sprintf("tblLubanTraceSpan%d", index)
}
