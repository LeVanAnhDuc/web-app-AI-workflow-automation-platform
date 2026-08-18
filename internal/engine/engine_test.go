package engine

import (
	"context"
	"testing"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// linearGraph is trigger -> middle -> end, the shape most tests below use.
func linearGraph(types ...string) domain.Graph {
	g := domain.Graph{}
	names := []string{"Start", "Middle", "End", "Tail"}
	for i, typ := range types {
		g.Nodes = append(g.Nodes, graphNode(nodeID(i+1), typ, names[i]))
		if i > 0 {
			g.Edges = append(g.Edges, graphEdge("e"+nodeID(i), nodeID(i), "", nodeID(i+1), ""))
		}
	}
	return g
}

func nodeID(i int) string { return "n" + string(rune('0'+i)) }

func TestRunLinearGraphSucceedsInTopologicalOrder(t *testing.T) {
	trigger, middle, end := fakeTrigger("trigger.test"), fakePass("pass.a"), fakePass("pass.b")
	reg := nodes.NewRegistry(trigger, middle, end)
	g := linearGraph("trigger.test", "pass.a", "pass.b")
	st := newFakeStore(g, domain.NewItem(map[string]any{"hello": "world"}))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")
	mustEqual(t, st.terminalOrder(), []string{"n1", "n2", "n3"}, "terminal rows in topological order")
	mustEqual(t, st.upsertStatuses(), []string{
		"n1:running", "n1:succeeded",
		"n2:running", "n2:succeeded",
		"n3:running", "n3:succeeded",
	}, "every node persists running before its terminal row")
	mustEqual(t, st.row("n3").Output[domain.MainHandle],
		[]domain.Item{domain.NewItem(map[string]any{"hello": "world"})},
		"the trigger payload reached the last node")
	if st.execution().StartedAt == nil || st.execution().FinishedAt == nil {
		t.Fatal("execution must record started_at and finished_at")
	}
}

func TestRunIfBranchingSkipsTheUntakenBranch(t *testing.T) {
	trigger, branch := fakeTrigger("trigger.test"), fakeBranch("if.test")
	yes, no := fakePass("pass.yes"), fakePass("pass.no")
	reg := nodes.NewRegistry(trigger, branch, yes, no)
	g := domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n1", "trigger.test", "Start"),
			graphNode("n2", "if.test", "If"),
			graphNode("n3", "pass.yes", "Then"),
			graphNode("n4", "pass.no", "Else"),
		},
		Edges: []domain.Edge{
			graphEdge("e1", "n1", "", "n2", ""),
			graphEdge("e2", "n2", nodes.HandleTrue, "n3", ""),
			graphEdge("e3", "n2", nodes.HandleFalse, "n4", ""),
		},
	}
	st := newFakeStore(g, domain.NewItem(map[string]any{"ok": true}))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")
	mustEqual(t, st.status("n3"), domain.StatusSucceeded, "the taken branch runs")
	mustEqual(t, st.status("n4"), domain.StatusSkipped, "the untaken branch is skipped")
	mustEqual(t, yes.callCount(), 1, "then-node calls")
	mustEqual(t, no.callCount(), 0, "else-node must never be executed")
}

func TestRunPerItemFansOutAndConcatenates(t *testing.T) {
	trigger, each := fakeTrigger("trigger.test"), fakePerItem("each.test")
	reg := nodes.NewRegistry(trigger, each)
	g := linearGraph("trigger.test", "each.test")
	st := newFakeStore(g,
		domain.NewItem(map[string]any{"n": 1}),
		domain.NewItem(map[string]any{"n": 2}),
		domain.NewItem(map[string]any{"n": 3}),
	)
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, each.callCount(), 3, "a perItem node is called once per item")
	out := st.row("n2").Output[domain.MainHandle]
	mustEqual(t, len(out), 3, "the calls' outputs concatenate")
	for i, item := range out {
		mustEqual(t, item.JSON["index"], i, "item index seen by the node")
		mustEqual(t, item.JSON["n"], i+1, "item payload")
	}
	mustEqual(t, each.call(1).ItemIndex, 1, "ec.ItemIndex is the loop index")
	mustEqual(t, len(each.call(1).Items), 3, "ec.Items stays the whole input set")
}

func TestRunRetryExhaustionFailsWithTheAttemptCount(t *testing.T) {
	trigger, flaky := fakeTrigger("trigger.test"), fakeFail("fail.test", "boom")
	reg := nodes.NewRegistry(trigger, flaky)
	g := linearGraph("trigger.test", "fail.test")
	g.Nodes[1].Settings = domain.NodeSettings{RetryOnFail: true, MaxTries: 3, WaitBetweenTriesMs: 250}
	st := newFakeStore(g)
	opts, slept := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, flaky.callCount(), 3, "maxTries attempts")
	mustEqual(t, *slept, []time.Duration{250 * time.Millisecond, 250 * time.Millisecond},
		"one wait between each pair of attempts")
	mustEqual(t, st.execution().Status, domain.StatusFailed, "execution status")
	mustEqual(t, st.status("n2"), domain.StatusFailed, "node status")

	execErr := st.execution().Error
	if execErr == nil {
		t.Fatal("a failed execution must record its error")
	}
	mustEqual(t, execErr.Attempts, 3, "attempts reached")
	mustEqual(t, execErr.Code, domain.ErrCodeHTTP, "the node's error code survives")
	mustEqual(t, execErr.NodeID, "n2", "error names the node")
	mustEqual(t, execErr.NodeName, "Middle", "error names the node")
	mustEqual(t, st.row("n2").Error.Attempts, 3, "the node row records the attempts too")
}

func TestRunContinueOnFailTurnsTheErrorIntoAnItem(t *testing.T) {
	trigger, flaky, after := fakeTrigger("trigger.test"), fakeFail("fail.test", "boom"), fakePass("pass.a")
	reg := nodes.NewRegistry(trigger, flaky, after)
	g := linearGraph("trigger.test", "fail.test", "pass.a")
	g.Nodes[1].Settings = domain.NodeSettings{ContinueOnFail: true}
	st := newFakeStore(g,
		domain.NewItem(map[string]any{"n": 1}),
		domain.NewItem(map[string]any{"n": 2}),
	)
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "the run continues")
	row := st.row("n2")
	mustEqual(t, row.Status, domain.StatusSucceeded, "continueOnFail keeps the row green")
	if row.Error == nil {
		t.Fatal("the error must still be recorded on the row")
	}
	mustEqual(t, row.Error.Message, "boom", "recorded error")
	mustEqual(t, row.Output[domain.MainHandle], []domain.Item{
		domain.NewItem(map[string]any{"error": "boom", "code": domain.ErrCodeHTTP}),
		domain.NewItem(map[string]any{"error": "boom", "code": domain.ErrCodeHTTP}),
	}, "one error item per input item")
	mustEqual(t, after.callCount(), 1, "the downstream node still runs")
	mustEqual(t, after.call(0).Items[0].JSON["error"], "boom", "and it sees the error item")
}

func TestRunResumeDoesNotReExecuteSucceededNodes(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	one, two, three := fakePass("pass.a"), fakePass("pass.b"), fakePass("pass.c")
	reg := nodes.NewRegistry(trigger, one, two, three)
	g := linearGraph("trigger.test", "pass.a", "pass.b", "pass.c")
	st := newFakeStore(g)

	// What a worker that crashed after node 2 left behind.
	done := []domain.Item{domain.NewItem(map[string]any{"from": "node 2"})}
	st.seedSucceeded(g.Nodes[0], map[string][]domain.Item{domain.MainHandle: {domain.NewItem(nil)}})
	st.seedSucceeded(g.Nodes[1], map[string][]domain.Item{domain.MainHandle: done})
	started := time.Date(2026, 8, 18, 11, 0, 0, 0, time.UTC)
	st.exec.StartedAt = &started
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")
	mustEqual(t, trigger.callCount(), 0, "node 1 must not run again")
	mustEqual(t, one.callCount(), 0, "node 2 must not run again")
	mustEqual(t, two.callCount(), 1, "node 3 runs")
	mustEqual(t, three.callCount(), 1, "node 4 runs")
	mustEqual(t, st.terminalOrder(), []string{"n3", "n4"}, "only the unfinished nodes are written")
	mustEqual(t, two.call(0).Items, done, "node 3 resumes from node 2's stored output")
	mustEqual(t, st.execution().StartedAt.Equal(started), true,
		"a resumed run keeps its original started_at")
}

func TestRunResumeFromNodeReRunsTheDownstreamNodes(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	one, two := fakePass("pass.a"), fakePass("pass.b")
	reg := nodes.NewRegistry(trigger, one, two)
	g := linearGraph("trigger.test", "pass.a", "pass.b")
	st := newFakeStore(g, domain.NewItem(map[string]any{"n": 1}))
	st.seedSucceeded(g.Nodes[0], map[string][]domain.Item{
		domain.MainHandle: {domain.NewItem(map[string]any{"n": 1})}})
	st.seedSucceeded(g.Nodes[1], map[string][]domain.Item{
		domain.MainHandle: {domain.NewItem(map[string]any{"stale": true})}})
	st.seedSucceeded(g.Nodes[2], map[string][]domain.Item{
		domain.MainHandle: {domain.NewItem(map[string]any{"stale": true})}})
	st.exec.ResumeFromNode = ptr("n2")
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, trigger.callCount(), 0, "the node before the resume point is reused")
	mustEqual(t, one.callCount(), 1, "the resume node re-runs even though a row says succeeded")
	mustEqual(t, two.callCount(), 1, "and so does everything downstream of it")
	mustEqual(t, st.terminalOrder(), []string{"n2", "n3"}, "rows rewritten")
}

func TestRunCancellationStopsBeforeTheNextNode(t *testing.T) {
	trigger, second := fakeTrigger("trigger.test"), fakePass("pass.a")
	reg := nodes.NewRegistry(trigger, second)
	g := linearGraph("trigger.test", "pass.a")
	st := newFakeStore(g)
	st.cancelAt = 2 // false before node 1, true before node 2
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusCancelled, "execution status")
	mustEqual(t, trigger.callCount(), 1, "node 1 had already run")
	mustEqual(t, second.callCount(), 0, "node 2 must not be executed")
	mustEqual(t, st.status("n2"), domain.Status(""), "node 2 has no row at all")
	if st.execution().FinishedAt == nil {
		t.Fatal("a cancelled execution is terminal and must record finished_at")
	}
}

func TestRunPerNodeTimeoutIsATimeoutError(t *testing.T) {
	trigger, slow := fakeTrigger("trigger.test"), fakeSlow("slow.test")
	reg := nodes.NewRegistry(trigger, slow)
	g := linearGraph("trigger.test", "slow.test")
	g.Nodes[1].Settings = domain.NodeSettings{TimeoutMs: 20}
	st := newFakeStore(g)
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusFailed, "execution status")
	mustEqual(t, st.execution().Error.Code, domain.ErrCodeTimeout, "execution error code")
	mustEqual(t, st.row("n2").Status, domain.StatusFailed, "node status")
	mustEqual(t, st.row("n2").Error.Code, domain.ErrCodeTimeout, "node error code")
	mustContain(t, st.row("n2").Error.Message, "timed out", "node error message")
}

func TestRunExecutionTimeoutFailsTheExecution(t *testing.T) {
	trigger, slow := fakeTrigger("trigger.test"), fakeSlow("slow.test")
	reg := nodes.NewRegistry(trigger, slow)
	g := linearGraph("trigger.test", "slow.test")
	st := newFakeStore(g)
	opts, _ := testOptions()
	opts.ExecutionTimeout = 20 * time.Millisecond // the node's own timeout is 60s

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusFailed, "execution status")
	mustEqual(t, st.execution().Error.Code, domain.ErrCodeTimeout, "execution error code")
	mustContain(t, st.execution().Error.Message, "execution timed out", "execution error message")
}

func TestRunPanicInANodeBecomesAnInternalError(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	boom := &fakeNode{desc: desc("panic.test", nodes.ModeOnce),
		exec: func(nodes.ExecContext) (nodes.Result, error) { panic("held together with tape") }}
	reg := nodes.NewRegistry(trigger, boom)
	g := linearGraph("trigger.test", "panic.test")
	st := newFakeStore(g)
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusFailed, "execution status")
	mustEqual(t, st.execution().Error.Code, domain.ErrCodeInternal, "execution error code")
	mustContain(t, st.execution().Error.Message, "tape", "the panic value is reported")
}

func TestRunDisabledNodeIsSkippedButPassesItsInputOn(t *testing.T) {
	trigger, middle, end := fakeTrigger("trigger.test"), fakePass("pass.a"), fakePass("pass.b")
	reg := nodes.NewRegistry(trigger, middle, end)
	g := linearGraph("trigger.test", "pass.a", "pass.b")
	g.Nodes[1].Disabled = true
	st := newFakeStore(g, domain.NewItem(map[string]any{"n": 1}))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.status("n2"), domain.StatusSkipped, "a disabled node is skipped")
	mustEqual(t, middle.callCount(), 0, "and never executed")
	mustEqual(t, st.status("n3"), domain.StatusSucceeded, "the chain continues")
	mustEqual(t, end.call(0).Items, []domain.Item{domain.NewItem(map[string]any{"n": 1})},
		"the disabled node forwarded its input")
}

func TestRunSkipsANonTriggerNodeWithNoIncomingEdges(t *testing.T) {
	trigger, second, orphan := fakeTrigger("trigger.test"), fakePass("pass.a"), fakePass("pass.b")
	reg := nodes.NewRegistry(trigger, second, orphan)
	g := linearGraph("trigger.test", "pass.a")
	g.Nodes = append(g.Nodes, graphNode("n3", "pass.b", "Orphan"))
	st := newFakeStore(g)
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")
	mustEqual(t, st.status("n3"), domain.StatusSkipped, "nothing can feed an orphan node")
	mustEqual(t, orphan.callCount(), 0, "so it is never executed")
}

func TestRunMergeNodeSeesBothInputsInHandleOrder(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	left, right, merge := fakePerItem("left.test"), fakePerItem("right.test"), fakeMerge("merge.test")
	reg := nodes.NewRegistry(trigger, left, right, merge)
	g := domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n1", "trigger.test", "Start"),
			graphNode("n2", "left.test", "Left"),
			graphNode("n3", "right.test", "Right"),
			graphNode("n4", "merge.test", "Merge"),
		},
		Edges: []domain.Edge{
			graphEdge("e1", "n1", "", "n2", ""),
			graphEdge("e2", "n1", "", "n3", ""),
			// Declared in reverse so the test proves the descriptor's handle
			// order wins, not the edge order.
			graphEdge("e3", "n3", "", "n4", nodes.HandleInput2),
			graphEdge("e4", "n2", "", "n4", nodes.HandleInput1),
		},
	}
	st := newFakeStore(g, domain.NewItem(map[string]any{"n": 1}))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	seen := merge.call(0)
	mustEqual(t, len(seen.Items), 2, "both inputs reach ec.Items")
	mustEqual(t, seen.Items[0].JSON["by"], "left.test", "input1 comes first")
	mustEqual(t, seen.Items[1].JSON["by"], "right.test", "input2 comes second")
	mustEqual(t, len(seen.Inputs[nodes.HandleInput1]), 1, "ec.Inputs is grouped by handle")
	mustEqual(t, len(seen.Inputs[nodes.HandleInput2]), 1, "ec.Inputs is grouped by handle")
}

func TestRunResolvesExpressionParametersPerItem(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	greeter := &fakeNode{
		desc: nodes.Descriptor{
			Type: "greet.test", Name: "greet", Mode: nodes.ModePerItem,
			Inputs: nodes.MainIn, Outputs: nodes.MainOut,
			Params: []nodes.ParamSpec{
				{Name: "greeting", Type: nodes.ParamString, SupportsExpression: true},
				{Name: "first", Type: nodes.ParamString, SupportsExpression: true},
			},
		},
		exec: func(ec nodes.ExecContext) (nodes.Result, error) {
			greeting, err := ec.Params.String("greeting")
			if err != nil {
				return nodes.Result{}, err
			}
			first, err := ec.Params.String("first")
			if err != nil {
				return nodes.Result{}, err
			}
			return nodes.Main(domain.NewItem(map[string]any{
				"greeting": greeting, "first": first, "exec": ec.ExecutionID,
			})), nil
		},
	}
	reg := nodes.NewRegistry(trigger, greeter)
	g := linearGraph("trigger.test", "greet.test")
	g.Nodes[1].Params = map[string]any{
		"greeting": "Hi {{ $json.name }} (#{{ $itemIndex }})",
		"first":    `{{ $node["Start"].json.name }}`,
	}
	st := newFakeStore(g,
		domain.NewItem(map[string]any{"name": "Ada"}),
		domain.NewItem(map[string]any{"name": "Bob"}),
	)
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	out := st.row("n2").Output[domain.MainHandle]
	mustEqual(t, len(out), 2, "one output item per input item")
	mustEqual(t, out[0].JSON["greeting"], "Hi Ada (#0)", "expressions see the current item")
	mustEqual(t, out[1].JSON["greeting"], "Hi Bob (#1)", "expressions see the current item")
	mustEqual(t, out[0].JSON["first"], "Ada", `$node["Start"] reads the upstream output`)
	mustEqual(t, out[0].JSON["exec"], "exec-1", "the execution id is in scope")
}

func TestRunValidationFailureMarksTheExecutionFailed(t *testing.T) {
	trigger, other := fakeTrigger("trigger.test"), fakeTrigger("trigger.other")
	reg := nodes.NewRegistry(trigger, other)
	g := domain.Graph{Nodes: []domain.GraphNode{
		graphNode("n1", "trigger.test", "Start"),
		graphNode("n2", "trigger.other", "Also start"),
	}}
	st := newFakeStore(g)
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"),
		"an unrunnable graph is not a queue error")

	mustEqual(t, st.execution().Status, domain.StatusFailed, "execution status")
	mustEqual(t, st.execution().Error.Code, domain.ErrCodeValidation, "execution error code")
	mustContain(t, st.execution().Error.Message, "trigger", "execution error message")
	mustEqual(t, len(st.upserts), 0, "no node was touched")
	mustEqual(t, trigger.callCount(), 0, "no node was executed")
}

func TestRunIsANoOpForATerminalExecution(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	reg := nodes.NewRegistry(trigger)
	st := newFakeStore(linearGraph("trigger.test"))
	st.exec.Status = domain.StatusSucceeded
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")
	mustEqual(t, len(st.upserts), 0, "a duplicate job must not re-run the workflow")
	mustEqual(t, len(st.patches), 0, "and must not touch the execution row")
}

func TestRunReturnsStoreErrorsSoTheQueueRetries(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	reg := nodes.NewRegistry(trigger)
	st := newFakeStore(linearGraph("trigger.test"))
	st.failUpsert = domain.ErrConflict
	opts, _ := testOptions()

	err := New(st, reg, opts).Run(context.Background(), "exec-1")
	mustError(t, err, "a store failure must be reported to the queue")
	mustContain(t, err.Error(), "conflict", "the cause is wrapped")
}

func TestRunNodeOncePersistsNothing(t *testing.T) {
	each := fakePerItem("each.test")
	reg := nodes.NewRegistry(each)
	st := newFakeStore(domain.Graph{})
	opts, _ := testOptions()
	node := graphNode("n1", "each.test", "Each")
	input := []domain.Item{
		domain.NewItem(map[string]any{"n": 1}),
		domain.NewItem(map[string]any{"n": 2}),
	}

	result, err := RunNodeOnce(context.Background(), reg, node, input, opts)
	mustNoError(t, err, "run node once")

	mustEqual(t, each.callCount(), 2, "perItem still loops the items")
	mustEqual(t, len(result.Get(domain.MainHandle)), 2, "outputs are returned")
	mustEqual(t, result.Get(domain.MainHandle)[1].JSON["n"], 2, "output payload")
	mustEqual(t, len(st.upserts), 0, "nothing is persisted")
	mustEqual(t, len(st.patches), 0, "nothing is persisted")
}

func TestRunNodeOnceReportsAnUnknownType(t *testing.T) {
	reg := nodes.NewRegistry()
	opts, _ := testOptions()
	_, err := RunNodeOnce(context.Background(), reg, graphNode("n1", "nope", "Nope"), nil, opts)
	mustError(t, err, "unknown node type")
	mustEqual(t, domain.AsNodeError(err).Code, domain.ErrCodeValidation, "error code")
}

func TestRunNodeOnceReturnsTheNodeError(t *testing.T) {
	flaky := fakeFail("fail.test", "boom")
	reg := nodes.NewRegistry(flaky)
	opts, _ := testOptions()
	node := graphNode("n1", "fail.test", "Flaky")
	node.Settings = domain.NodeSettings{RetryOnFail: true, MaxTries: 2}

	_, err := RunNodeOnce(context.Background(), reg, node, nil, opts)
	mustError(t, err, "node failure")
	mustEqual(t, flaky.callCount(), 2, "the test endpoint honours retries too")
	mustEqual(t, domain.AsNodeError(err).NodeName, "Flaky", "the error names the node")
}
