"use client";

import clsx from "clsx";
import { Fragment, useMemo, useState } from "react";
import { AgentOutputView } from "@/components/agent/AgentTranscript";
import { hasAgentTranscript } from "@/components/agent/transcript";
import { Button, ErrorNotice, Field, IconButton, Input, JsonView, Toggle } from "@/components/ui";
import { Icons, NodeIcon } from "@/components/ui/icons";
import { nodeTypes } from "@/lib/api";
import { useEditorStore } from "@/lib/editorStore";
import { previewExpression, hasExpression } from "@/lib/expressionPreview";
import {
  acceptsTools,
  isParamVisible,
  isToolOnlyNode,
  keyValuePairs,
  toolProvidersOf,
  upstreamNames,
  type KeyValuePair,
} from "@/lib/graph";
import type { Graph, GraphNode, Item, NodeDescriptor, NodeExecution, NodeSettings } from "@/lib/types";
import { InlineEdit } from "./InlineEdit";
import { ParamField } from "./ParamFields";

type Tab = "parameters" | "settings" | "output";

/** The `keyValue` param an agent names its tools in. Annotated rather than
 *  special-cased: any node type declaring a tool input gets the same list. */
const TOOL_DESCRIPTIONS_PARAM = "toolDescriptions";

const tabs: { id: Tab; label: string }[] = [
  { id: "parameters", label: "Parameters" },
  { id: "settings", label: "Settings" },
  { id: "output", label: "Output" },
];

/**
 * The generated node form. Every control comes from the selected node's
 * descriptor, so a node added on the Go side shows up here fully configurable
 * with no frontend change — the point of the whole node-registry design.
 */
export function ConfigDrawer() {
  const graph = useEditorStore((s) => s.graph);
  const descriptors = useEditorStore((s) => s.descriptors);
  const selectedNodeId = useEditorStore((s) => s.selectedNodeId);
  const nodeExecutions = useEditorStore((s) => s.nodeExecutions);
  const testResults = useEditorStore((s) => s.testResults);
  const setParam = useEditorStore((s) => s.setParam);
  const updateNode = useEditorStore((s) => s.updateNode);
  const renameNode = useEditorStore((s) => s.renameNode);
  const select = useEditorStore((s) => s.select);
  const setTestResult = useEditorStore((s) => s.setTestResult);

  const [tab, setTab] = useState<Tab>("parameters");
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState<string | null>(null);

  const node = graph.nodes.find((n) => n.id === selectedNodeId);
  const descriptor: NodeDescriptor | undefined = node ? descriptors[node.type] : undefined;

  // Run data feeding both the expression previews and the Test step input.
  const context = useMemo(() => {
    if (!node) return { json: {}, nodes: {}, inputItems: [{ json: {} }] as Item[] };

    const byName: Record<string, Record<string, unknown>> = {};
    for (const ne of nodeExecutions) byName[ne.nodeName] = firstOutput(ne)?.json ?? {};

    const own = nodeExecutions.find((n) => n.nodeId === node.id);
    const nearestUpstream = upstreamNames(graph, node.id)[0];
    const upstream = nodeExecutions.find((n) => n.nodeName === nearestUpstream);

    const inputItems: Item[] =
      own?.input ?? upstream?.output?.main ?? ([{ json: {} }] as Item[]);

    return { json: inputItems[0]?.json ?? {}, nodes: byName, inputItems };
  }, [graph, node, nodeExecutions]);

  if (!node) return null;

  const preview = (template: string): string | null =>
    hasExpression(template)
      ? previewExpression(template, { json: context.json, nodes: context.nodes })
      : null;

  const visibleParams = (descriptor?.Params ?? []).filter((p) =>
    isParamVisible(p, node.params),
  );

  const own = nodeExecutions.find((n) => n.nodeId === node.id);
  const testResult = testResults[node.id];

  const runTest = async () => {
    setTesting(true);
    setTestError(null);
    setTab("output");
    try {
      const result = await nodeTypes.test(node.type, node.params, context.inputItems);
      setTestResult(node.id, result);
    } catch (err) {
      setTestError(err instanceof Error ? err.message : "The test request failed.");
    } finally {
      setTesting(false);
    }
  };

  return (
    <aside
      aria-label={`Configuration for ${node.name}`}
      className="flex w-90 shrink-0 flex-col border-l border-line bg-panel"
    >
      <div className="flex items-center gap-3 px-4.5 py-4">
        <span
          className={clsx(
            "flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-[10px]",
            descriptor?.IsTrigger ? "bg-accent/15 text-accent-2" : "bg-white/6 text-ink-2",
          )}
        >
          <NodeIcon name={descriptor?.Icon ?? "table"} size={16} />
        </span>
        <div className="flex min-w-0 grow flex-col gap-0.5">
          <InlineEdit
            value={node.name}
            ariaLabel="Node name"
            onSave={(next) => renameNode(node.id, next)}
            className="text-[14.5px] font-bold"
            inputClassName="w-full"
          />
          <span className="truncate font-mono text-[10px] text-ink-4">{node.type}</span>
        </div>
        <IconButton aria-label="Close configuration" onClick={() => select(null)}>
          <Icons.close size={16} />
        </IconButton>
      </div>

      <div role="tablist" aria-label="Node configuration" className="flex gap-[5px] px-3.5 pb-3.5">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => setTab(t.id)}
            className={clsx(
              "rounded-lg px-3.5 py-[7px] text-[12.5px]",
              tab === t.id ? "bg-white/7 font-semibold text-ink" : "text-ink-4 hover:text-ink-2",
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="flex min-h-0 grow flex-col gap-[18px] overflow-y-auto px-4.5 pb-4">
        {tab === "parameters" && (
          <>
            {descriptor?.Credential && (
              <div className="flex items-start gap-2.5 rounded-[10px] border border-line bg-white/3 px-3.5 py-3">
                <Icons.lock size={14} className="mt-px shrink-0 text-ink-4" />
                <p className="text-xs leading-relaxed text-ink-3">
                  Needs a <span className="font-mono">{descriptor.Credential}</span> credential.
                  The credential vault arrives in phase 3.
                </p>
              </div>
            )}
            {!descriptor && (
              <ErrorNotice>
                No descriptor for <span className="font-mono">{node.type}</span>. The API may be
                offline, or this node type was removed.
              </ErrorNotice>
            )}
            {visibleParams.map((spec) => (
              <Fragment key={spec.Name}>
                {/* Descriptions are keyed by node name, so the names actually
                    wired in have to be visible while writing them. */}
                {spec.Name === TOOL_DESCRIPTIONS_PARAM && acceptsTools(descriptor) && (
                  <ToolWiring
                    graph={graph}
                    node={node}
                    onDescribe={(name) =>
                      setParam(node.id, TOOL_DESCRIPTIONS_PARAM, [
                        ...keyValuePairs(node.params[TOOL_DESCRIPTIONS_PARAM]),
                        { key: name, value: "" },
                      ])
                    }
                  />
                )}
                <ParamField
                  spec={spec}
                  value={node.params[spec.Name]}
                  preview={preview}
                  onChange={(next) => setParam(node.id, spec.Name, next)}
                />
              </Fragment>
            ))}
            {descriptor && visibleParams.length === 0 && (
              <p className="text-xs text-ink-4">This node has nothing to configure.</p>
            )}
          </>
        )}

        {tab === "settings" && (
          <SettingsTab
            settings={node.settings}
            onChange={(patch) =>
              updateNode(node.id, { settings: { ...node.settings, ...patch } })
            }
          />
        )}

        {tab === "output" && (
          <OutputTab
            execution={own}
            testing={testing}
            testError={testError}
            testOutputs={testResult?.outputs}
            testFailure={testResult?.error?.message}
          />
        )}
      </div>

      <div className="border-t border-line px-4.5 py-4">
        <Button
          variant="accentSoft"
          className="w-full"
          disabled={testing || !descriptor}
          onClick={runTest}
        >
          <Icons.play size={12} />
          {testing ? "Testing…" : "Test step"}
        </Button>
      </div>
    </aside>
  );
}

function SettingsTab({
  settings,
  onChange,
}: {
  settings: NodeSettings;
  onChange: (patch: Partial<NodeSettings>) => void;
}) {
  return (
    <>
      <SettingRow
        label="Retry on fail"
        description="Re-run the whole node when it errors."
        checked={settings.retryOnFail}
        onChange={(retryOnFail) => onChange({ retryOnFail })}
      />
      {settings.retryOnFail && (
        <>
          <Field label="Max tries">
            <Input
              type="number"
              min={1}
              value={settings.maxTries}
              className="font-mono"
              onChange={(e) => onChange({ maxTries: numberOr(e.target.value, 3) })}
            />
          </Field>
          <Field label="Wait between tries (ms)">
            <Input
              type="number"
              min={0}
              value={settings.waitBetweenTriesMs}
              className="font-mono"
              onChange={(e) => onChange({ waitBetweenTriesMs: numberOr(e.target.value, 1000) })}
            />
          </Field>
        </>
      )}
      <SettingRow
        label="Continue on fail"
        description="Turn the error into an item and carry on."
        checked={settings.continueOnFail}
        onChange={(continueOnFail) => onChange({ continueOnFail })}
      />
      <Field label="Timeout (ms)">
        <Input
          type="number"
          min={0}
          value={settings.timeoutMs}
          className="font-mono"
          onChange={(e) => onChange({ timeoutMs: numberOr(e.target.value, 60000) })}
        />
      </Field>
    </>
  );
}

function SettingRow({
  label,
  description,
  checked,
  onChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  onChange: (next: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <div className="flex flex-col gap-1">
        <span className="text-xs font-semibold text-ink-3">{label}</span>
        <span className="text-[11px] text-ink-4">{description}</span>
      </div>
      <Toggle checked={checked} onChange={onChange} label={label} />
    </div>
  );
}

function OutputTab({
  execution,
  testing,
  testError,
  testOutputs,
  testFailure,
}: {
  execution?: NodeExecution;
  testing: boolean;
  testError: string | null;
  testOutputs?: Record<string, Item[]>;
  testFailure?: string;
}) {
  if (testing) return <p className="text-xs text-ink-4">Running this step…</p>;
  if (testError) return <ErrorNotice>{testError}</ErrorNotice>;
  if (testFailure) return <ErrorNotice>{testFailure}</ErrorNotice>;

  const outputs = testOutputs ?? execution?.output;

  if (execution?.error && !testOutputs) {
    return (
      <>
        <ErrorNotice>{execution.error.message}</ErrorNotice>
        <JsonView value={execution.error} />
      </>
    );
  }

  if (!outputs) {
    return (
      <p className="text-xs leading-relaxed text-ink-4">
        Nothing yet. Run the workflow, or use “Test step” to execute just this node with the
        upstream node’s last output.
      </p>
    );
  }

  const handles = Object.entries(outputs);
  return (
    <>
      {handles.map(([handle, items]) => (
        <div key={handle} className="flex min-w-0 flex-col gap-2">
          {handles.length > 1 && (
            <span className="font-mono text-[10.5px] text-ink-4">
              {handle} · {items.length} item{items.length === 1 ? "" : "s"}
            </span>
          )}
          {/* An agent item is mostly transcript, so it gets the reader that can
              show it; everything else stays the compact JSON array. */}
          {hasAgentTranscript(items) ? (
            items.map((item, i) => (
              <div key={i} className="flex min-w-0 flex-col gap-1.5">
                {items.length > 1 && (
                  <span className="font-mono text-[10.5px] text-ink-5">item {i + 1}</span>
                )}
                <AgentOutputView item={item} className="max-h-[420px]" />
              </div>
            ))
          ) : (
            <JsonView value={items.map((i) => i.json)} className="max-h-[420px]" />
          )}
        </div>
      ))}
    </>
  );
}

/**
 * The nodes wired into this node tool handle, next to the editor that has to
 * name them. Without it the author is describing tools from memory, and a key
 * that matches no node name is silently ignored at run time.
 */
function ToolWiring({
  graph,
  node,
  onDescribe,
}: {
  graph: Graph;
  node: GraphNode;
  onDescribe: (name: string) => void;
}) {
  const providers = toolProvidersOf(graph, node.id);
  const rows: KeyValuePair[] = keyValuePairs(node.params[TOOL_DESCRIPTIONS_PARAM]);

  if (providers.length === 0) {
    return (
      <div className="rounded-[10px] border border-dashed border-line-strong px-3.5 py-3">
        <p className="text-[11.5px] leading-relaxed text-ink-4">
          Nothing is wired to the Tools input. Drag a connection from another node output onto
          the square handle under this card and it becomes a tool this node can call.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2 rounded-[10px] border border-line bg-white/3 px-3 py-2.5">
      <span className="text-[11px] font-bold tracking-[0.06em] text-ink-5">
        WIRED TO THE TOOLS INPUT
      </span>
      <ul className="flex flex-col gap-1.5">
        {providers.map((provider) => {
          const row = rows.find((r) => r.key === provider.name);
          const described = Boolean(row && row.value.trim() !== "");
          return (
            <li key={provider.id} className="flex items-center gap-2">
              <span className="truncate font-mono text-[11.5px] text-ink-2">{provider.name}</span>
              <span className="shrink-0 font-mono text-[10px] text-ink-5">
                {isToolOnlyNode(graph, provider.id) ? "tool only" : "also in the flow"}
              </span>
              {described ? (
                <span className="ml-auto shrink-0 font-mono text-[10px] text-success">
                  described
                </span>
              ) : row ? (
                <span className="ml-auto shrink-0 font-mono text-[10px] text-warning">
                  needs a description
                </span>
              ) : (
                <button
                  type="button"
                  onClick={() => onDescribe(provider.name)}
                  aria-label={`Add a description row for ${provider.name}`}
                  className="ml-auto shrink-0 rounded-md bg-accent/14 px-2 py-[2px] text-[10.5px] font-semibold text-accent-2 hover:bg-accent/22 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent"
                >
                  + describe
                </button>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function firstOutput(execution: NodeExecution): Item | undefined {
  const outputs = execution.output;
  if (!outputs) return undefined;
  return outputs.main?.[0] ?? Object.values(outputs)[0]?.[0];
}

function numberOr(raw: string, fallback: number): number {
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : fallback;
}
