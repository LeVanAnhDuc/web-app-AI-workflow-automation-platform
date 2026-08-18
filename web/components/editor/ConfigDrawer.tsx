"use client";

import clsx from "clsx";
import { useMemo, useState } from "react";
import { Button, ErrorNotice, Field, IconButton, Input, JsonView, Toggle } from "@/components/ui";
import { Icons, NodeIcon } from "@/components/ui/icons";
import { nodeTypes } from "@/lib/api";
import { useEditorStore } from "@/lib/editorStore";
import { previewExpression, hasExpression } from "@/lib/expressionPreview";
import { isParamVisible, upstreamNames } from "@/lib/graph";
import type { Item, NodeDescriptor, NodeExecution, NodeSettings } from "@/lib/types";
import { InlineEdit } from "./InlineEdit";
import { ParamField } from "./ParamFields";

type Tab = "parameters" | "settings" | "output";

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
              <ParamField
                key={spec.Name}
                spec={spec}
                value={node.params[spec.Name]}
                preview={preview}
                onChange={(next) => setParam(node.id, spec.Name, next)}
              />
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
        <div key={handle} className="flex flex-col gap-2">
          {handles.length > 1 && (
            <span className="font-mono text-[10.5px] text-ink-4">
              {handle} · {items.length} item{items.length === 1 ? "" : "s"}
            </span>
          )}
          <JsonView value={items.map((i) => i.json)} className="max-h-[420px]" />
        </div>
      ))}
    </>
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
