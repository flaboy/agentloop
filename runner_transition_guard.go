package agentloop

import "fmt"

type RunnerTransitionGuard struct {
	allowed map[RunnerState]map[RunnerEvent]RunnerState
}

func NewRunnerTransitionGuard() RunnerTransitionGuard {
	return RunnerTransitionGuard{
		allowed: map[RunnerState]map[RunnerEvent]RunnerState{
			RunnerStateIdle: {
				RunnerEventRunStarted: RunnerStatePreparingContext,
			},
			RunnerStatePreparingContext: {
				RunnerEventContextBuilt: RunnerStateCallingModel,
				RunnerEventRunFailed:    RunnerStateFailed,
			},
			RunnerStateCallingModel: {
				RunnerEventModelRequest:      RunnerStateCallingModel,
				RunnerEventModelResponse:     RunnerStateCallingModel,
				RunnerEventToolCallBegin:     RunnerStateExecutingTools,
				RunnerEventRoundtripPrepared: RunnerStatePreparingRoundtrip,
				RunnerEventRunCompleted:      RunnerStateCompleted,
				RunnerEventRunFailed:         RunnerStateFailed,
			},
			RunnerStateExecutingTools: {
				RunnerEventToolCallEnd:       RunnerStateExecutingTools,
				RunnerEventToolCallBegin:     RunnerStateExecutingTools,
				RunnerEventRoundtripPrepared: RunnerStatePreparingRoundtrip,
				RunnerEventRunFailed:         RunnerStateFailed,
			},
			RunnerStatePreparingRoundtrip: {
				RunnerEventContextRewritten: RunnerStateCallingModel,
				RunnerEventRunFailed:        RunnerStateFailed,
			},
			RunnerStateCompleted: {},
			RunnerStateFailed:    {},
		},
	}
}

func (g RunnerTransitionGuard) Validate(from RunnerState, event RunnerEvent, to RunnerState) error {
	stateRules, ok := g.allowed[from]
	if !ok {
		return fmt.Errorf("runner transition rejected: unknown from state=%q event=%q to=%q", from, event, to)
	}
	expectedTo, ok := stateRules[event]
	if !ok {
		return fmt.Errorf("runner transition rejected: event=%q not allowed from state=%q (to=%q)", event, from, to)
	}
	if expectedTo != to {
		return fmt.Errorf("runner transition rejected: expected to=%q for state=%q event=%q, got to=%q", expectedTo, from, event, to)
	}
	return nil
}
