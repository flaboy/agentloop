package agentloop

import "testing"

func TestTransitionGuard_RejectsInvalidHop(t *testing.T) {
	guard := NewRunnerTransitionGuard()
	err := guard.Validate(RunnerStateIdle, RunnerEventModelResponse, RunnerStateCallingModel)
	if err == nil {
		t.Fatal("expected invalid transition error")
	}
}

func TestTransitionGuard_AcceptsValidPath(t *testing.T) {
	guard := NewRunnerTransitionGuard()
	steps := []TransitionRecord{
		{From: RunnerStateIdle, Event: RunnerEventRunStarted, To: RunnerStatePreparingContext},
		{From: RunnerStatePreparingContext, Event: RunnerEventContextBuilt, To: RunnerStateCallingModel},
		{From: RunnerStateCallingModel, Event: RunnerEventModelRequest, To: RunnerStateCallingModel},
		{From: RunnerStateCallingModel, Event: RunnerEventRunCompleted, To: RunnerStateCompleted},
	}
	for _, step := range steps {
		if err := guard.Validate(step.From, step.Event, step.To); err != nil {
			t.Fatalf("unexpected validation error: %v step=%+v", err, step)
		}
	}
}
