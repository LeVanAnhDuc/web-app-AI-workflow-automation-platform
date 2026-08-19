import { Suspense } from "react";
import { CredentialsScreen } from "@/components/credentials/CredentialsScreen";

export const metadata = { title: "Credentials · Flowgrid" };

export default function CredentialsPage() {
  // The screen reads the OAuth callback's query string, which Next only allows
  // inside a Suspense boundary — without one the whole route opts out of
  // prerendering.
  return (
    <Suspense fallback={<div className="min-h-screen bg-surface" />}>
      <CredentialsScreen />
    </Suspense>
  );
}
