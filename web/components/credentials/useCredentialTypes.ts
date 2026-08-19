"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { credentialTypes as credentialTypesApi } from "@/lib/api";
import type { CredentialType } from "@/lib/types";

export const CREDENTIAL_TYPES_KEY = ["credentialTypes"] as const;

/**
 * The credential registry, shared by the list screen, the modal and the node
 * drawer. It changes only with a deploy, so it is cached for the session rather
 * than refetched per screen — and every consumer needs the same `redirectUri`.
 */
export function useCredentialTypes() {
  const query = useQuery({
    queryKey: CREDENTIAL_TYPES_KEY,
    queryFn: credentialTypesApi.list,
    staleTime: Infinity,
  });

  const list = useMemo<CredentialType[]>(
    () => query.data?.credentialTypes ?? [],
    [query.data],
  );

  const byType = useMemo(() => {
    const map = new Map<string, CredentialType>();
    for (const t of list) map.set(t.type, t);
    return map;
  }, [list]);

  return {
    types: list,
    byType,
    /** The URL the user must register with the provider, verbatim. */
    redirectUri: query.data?.redirectUri ?? "",
    isPending: query.isPending,
    isError: query.isError,
    error: query.error,
    refetch: query.refetch,
  };
}
