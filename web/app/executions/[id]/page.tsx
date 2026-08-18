import { ExecutionDetail } from "@/components/executions/ExecutionDetail";

/**
 * A thin server shell: it only unwraps the route param so the screen itself can
 * be a client component that streams its own updates.
 */
export default async function ExecutionDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <ExecutionDetail id={id} />;
}
