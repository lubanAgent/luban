// Package agents imports Agent packages that contain Go functions or Tools.
// Pure configuration Agents only need an agent.json, without a Go import.
package agents

import (
	_ "github.com/lubanAgent/luban/agents/admissions"
	_ "github.com/lubanAgent/luban/agents/callsummary"
)
