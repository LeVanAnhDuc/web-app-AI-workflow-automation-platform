import { WorkflowEditor } from "@/components/editor/WorkflowEditor";

/**
 * The editor fetches everything client-side through TanStack Query, so this
 * route renders without the Go API being reachable — which is what makes
 * `next build` safe when only the web app is running.
 */
export default async function WorkflowEditorPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <WorkflowEditor id={id} />;
}
