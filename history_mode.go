package agentloop

import "strings"

type HistoryMode string

const (
	HistoryModeLocalReplay   HistoryMode = "local_replay"
	HistoryModeProviderState HistoryMode = "provider_state"
	HistoryModeHybridAuto    HistoryMode = "hybrid_auto"
)

func normalizeHistoryMode(mode HistoryMode) HistoryMode {
	switch HistoryMode(strings.TrimSpace(string(mode))) {
	case HistoryModeProviderState:
		return HistoryModeProviderState
	case HistoryModeHybridAuto:
		return HistoryModeHybridAuto
	default:
		return HistoryModeLocalReplay
	}
}

