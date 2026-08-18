"use client";

import { ReactFlowProvider, useReactFlow } from "@xyflow/react";
import { useMutation, useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { Button, ErrorNotice, Spinner } from "@/components/ui";
import { executions, nodeTypes, workflows } from "@/lib/api";
import { isRunning, useEditorStore } from "@/lib/editorStore";
import { validateGraph } from "@/lib/graph";
import { ConfigDrawer } from "./ConfigDrawer";
import { EditorCanvas } from "./EditorCanvas";
import { EditorTopbar } from "./EditorTopbar";
import { ExecutionLogPanel } from "./ExecutionLogPanel";
import { NodePalette } from "./NodePalette";
import { NodePicker } from "./NodePicker";
import { useExecutionStream } from "./useExecutionStream";

export function WorkflowEditor({ id }: { id: string }) {
  // `useReactFlow` is needed by the shell (drop points, "add at centre"), so the
  // provider has to sit above it rather than around the canvas alone.
  return (
    <ReactFlowProvider>
      <EditorShell id={id} />
    </ReactFlowProvider>
  );
}

function EditorShell({ id }: { id: string }) {
  const detail = useQuery({
    queryKey: ["workflow", id],
    queryFn: () => workflows.get(id),
    retry: false,
    staleTime: Infinity,
  });
  const types = useQuery({
    queryKey: ["node-types"],
    queryFn: nodeTypes.list,
    retry: false,
    staleTime: Infinity,
  });

  const graph = useEditorStore((s) => s.graph);
  const descriptorList = useEditorStore((s) => s.descriptorList);
  const dirty = useEditorStore((s) => s.dirty);
  const version = useEditorStore((s) => s.version);
  const currentExecutionId = useEditorStore((s) => s.currentExecutionId);
  const running = useEditorStore(isRunning);
  const load = useEditorStore((s) => s.load);
  const setDescriptors = useEditorStore((s) => s.setDescriptors);
  const addNode = useEditorStore((s) => s.addNode);
  const deleteSelection = useEditorStore((s) => s.deleteSelection);
  const markSaved = useEditorStore((s) => s.markSaved);
  const startRun = useEditorStore((s) => s.startRun);
  const applyStreamEvent = useEditorStore((s) => s.applyStreamEvent);

  const [name, setName] = useState("");
  const [active, setActive] = useState(false);
  const [problems, setProblems] = useState<string[]>([]);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const canvasRef = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition } = useReactFlow();

  // The graph is loaded into the store exactly once per workflow. A later
  // refetch must not blow away unsaved edits or the current selection.
  const loadedFor = useRef<string | null>(null);
  useEffect(() => {
    if (!detail.data || loadedFor.current === id) return;
    loadedFor.current = id;
    load({
      graph: detail.data.graph ?? { nodes: [], edges: [] },
      version: detail.data.version,
    });
    setName(detail.data.workflow.name);
    setActive(detail.data.workflow.active);
  }, [detail.data, id, load]);

  useEffect(() => {
    if (types.data) setDescriptors(types.data.nodeTypes ?? []);
  }, [types.data, setDescriptors]);

  useExecutionStream(currentExecutionId, applyStreamEvent, {
    onError: () => setActionError("Lost the connection to the execution stream."),
  });

  const patch = useMutation({
    mutationFn: (body: { name?: string; active?: boolean }) => workflows.patch(id, body),
    onError: (err) => {
      // The optimistic edit is rolled back from the server's copy, which is the
      // only value we know to be true.
      if (detail.data) {
        setName(detail.data.workflow.name);
        setActive(detail.data.workflow.active);
      }
      setActionError(message(err, "Could not update the workflow."));
    },
    onSuccess: (data) => {
      setName(data.workflow.name);
      setActive(data.workflow.active);
    },
  });

  const save = useMutation({
    mutationFn: () => workflows.saveVersion(id, useEditorStore.getState().graph),
    onSuccess: ({ version: saved }) => {
      markSaved(saved.version);
      setProblems([]);
      setActionError(null);
    },
    onError: (err) => setActionError(message(err, "Could not save this version.")),
  });

  const run = useMutation({
    mutationFn: () => workflows.run(id),
    onSuccess: ({ execution }) => {
      startRun(execution);
      setActionError(null);
    },
    onError: (err) => setActionError(message(err, "Could not start a run.")),
  });

  const stop = useMutation({
    mutationFn: () => executions.cancel(currentExecutionId ?? ""),
    onSuccess: ({ execution }) => applyStreamEvent({ type: "execution", execution }),
    onError: (err) => setActionError(message(err, "Could not cancel the run.")),
  });

  const handleSave = useCallback(() => {
    const state = useEditorStore.getState();
    if (!state.dirty || save.isPending) return;
    const found = validateGraph(state.graph, state.descriptors);
    if (found.length > 0) {
      setProblems(found);
      return;
    }
    setProblems([]);
    save.mutate();
  }, [save]);

  const addAtCentre = useCallback(
    (type: string) => {
      const rect = canvasRef.current?.getBoundingClientRect();
      const position = rect
        ? screenToFlowPosition({ x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 })
        : undefined;
      addNode(type, position);
    },
    [addNode, screenToFlowPosition],
  );

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        handleSave();
        return;
      }
      // Never steal keys from a field the user is typing in — Tab must still
      // move focus inside the palette, the drawer and the picker.
      if (pickerOpen || isEditableTarget(e.target)) return;

      if (e.key === "Tab") {
        e.preventDefault();
        setPickerOpen(true);
      } else if (e.key === "Delete" || e.key === "Backspace") {
        e.preventDefault();
        deleteSelection();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [deleteSelection, handleSave, pickerOpen]);

  if (detail.isPending) {
    return (
      <div className="flex h-screen items-center justify-center gap-3 text-ink-4">
        <Spinner className="h-4 w-4" />
        <span className="text-[13px]">Loading the workflow…</span>
      </div>
    );
  }

  if (detail.isError || !detail.data) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-4 px-6">
        <div className="w-full max-w-md">
          <ErrorNotice>
            {message(detail.error, "This workflow could not be loaded.")} The API may not be
            running.
          </ErrorNotice>
        </div>
        <Link href="/workflows">
          <Button variant="secondary">Back to workflows</Button>
        </Link>
      </div>
    );
  }

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-surface">
      <EditorTopbar
        name={name}
        version={version}
        active={active}
        dirty={dirty}
        saving={save.isPending}
        running={running}
        starting={run.isPending}
        problems={problems}
        onRename={(next) => {
          setName(next);
          patch.mutate({ name: next });
        }}
        onToggleActive={(next) => {
          setActive(next);
          patch.mutate({ active: next });
        }}
        onSave={handleSave}
        onDismissProblems={() => setProblems([])}
        onRun={() => run.mutate()}
        onStop={() => stop.mutate()}
      />

      {(actionError || types.isError) && (
        <div className="border-b border-line px-4.5 py-2.5">
          <ErrorNotice>
            {actionError ??
              "Node types could not be loaded, so the palette and the generated form are empty."}
          </ErrorNotice>
        </div>
      )}

      <div className="flex min-h-0 grow">
        <NodePalette descriptors={descriptorList} onAdd={addAtCentre} />
        <div ref={canvasRef} className="relative min-w-0 grow">
          <EditorCanvas onOpenPicker={() => setPickerOpen(true)} />
        </div>
        <ConfigDrawer />
      </div>

      <ExecutionLogPanel />

      <NodePicker
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        descriptors={descriptorList}
        graph={graph}
        onAdd={addAtCentre}
      />

    </div>
  );
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return (
    tag === "INPUT" ||
    tag === "TEXTAREA" ||
    tag === "SELECT" ||
    target.isContentEditable ||
    target.closest("[role='dialog']") !== null
  );
}

function message(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}
