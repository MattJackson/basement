// v1.11.0.14 — Mutation cache-invalidation audit.
//
// Operator-reported bug: deleting a UserRegion key on /files/keys
// left the row visible until a manual page refresh. Root cause: the
// `useDeleteUserRegion` mutation hook didn't invalidate the
// ["user","regions"] cache that `useUserRegions` reads from, so the
// React Query cache stayed warm with the deleted row until staleTime
// or a focus refetch fired.
//
// Same shape bug class likely exists across every mutation in queries.ts;
// this suite exercises the pattern for the highest-bug-risk mutations
// (delete + write paths the operator interacts with directly) by spying
// on QueryClient.invalidateQueries and asserting the expected queryKey
// shows up after a successful mutation.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import React from "react";

// Stub the openapi-fetch client surface up-front so the hooks that go
// through it (backups, syncs, service accounts via raw fetch, etc.)
// don't try to hit the network. We mutate the implementation per-test
// via vi.mocked on the named exports.
vi.mock("@/shared/api/client", () => ({
  client: {
    GET: vi.fn(),
    POST: vi.fn(),
    PUT: vi.fn(),
    PATCH: vi.fn(),
    DELETE: vi.fn(),
  },
}));

import {
  useDeleteUserRegion,
  useCreateUserKey,
  useBulkCreateUserKeys,
  useRotateUserRegion,
  useDeleteWebhook,
  useEnableWebhook,
  useDisableWebhook,
  useDeleteFederation,
  useFailoverFederation,
  useResyncFederation,
  useRevokeInvite,
  useCreateInvite,
  usePutBucketVersioning,
  useDeleteBucketEncryption,
  usePutBucketEncryption,
  useDeleteUserSync,
  useDeleteServiceAccount,
  useRotateServiceAccount,
  useDeleteUserBackup,
  useRunUserBackup,
  useUpdateUserBackup,
  useDeleteRole,
  useUpsertRole,
} from "@/shared/api/queries";
import { client } from "@/shared/api/client";

function withProviders(qc: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

function freshClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

// stubFetch installs a global fetch mock that always returns the given
// status + JSON body. Each test resets via vi.restoreAllMocks() in
// beforeEach. We don't bother with URL-routing — the assertion is on
// invalidateQueries, not on the request shape.
function stubFetch(status: number, body: unknown) {
  const ok = status >= 200 && status < 300;
  globalThis.fetch = vi.fn(async () =>
    ({
      ok,
      status,
      json: async () => body,
    }) as unknown as Response,
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
});

// invalidationCases — every (hook, mutate-input, expected-queryKey)
// triple drives one parameterised test below. The keys mirror the
// `useXxx()` reads they're paired with in queries.ts; if a hook
// changes its read key, the matching mutation case here flags it.
type Case = {
  name: string;
  hook: () => { mutateAsync: (...args: any[]) => Promise<any> };
  input?: unknown;
  expectedKeys: string[][];
  // For hooks that hit openapi-fetch instead of bare fetch, swap the
  // fetch stub for the client method stub.
  setup?: () => void;
};

const cases: Case[] = [
  {
    name: "useDeleteUserRegion invalidates [user,regions] (operator's repro)",
    hook: () => useDeleteUserRegion(),
    input: "smoke-region-123",
    expectedKeys: [["user", "regions"]],
  },
  {
    name: "useCreateUserKey invalidates [user,regions]",
    hook: () => useCreateUserKey(),
    input: {
      alias: "smoke",
      endpoint: "https://s3.example/",
      accessKeyId: "x",
      secretKey: "y",
    },
    expectedKeys: [["user", "regions"]],
  },
  {
    name: "useBulkCreateUserKeys invalidates [user,regions]",
    hook: () => useBulkCreateUserKeys(),
    input: [],
    expectedKeys: [["user", "regions"]],
    setup: () => stubFetch(200, { created: [], errors: [] }),
  },
  {
    name: "useRotateUserRegion invalidates [user,regions]",
    hook: () => useRotateUserRegion(),
    input: { regionId: "r1", accessKeyId: "a", secretKey: "s" },
    expectedKeys: [["user", "regions"], ["user", "regions", "r1"]],
  },
  {
    name: "useDeleteWebhook invalidates [user,webhooks]",
    hook: () => useDeleteWebhook(),
    input: "wh-1",
    expectedKeys: [["user", "webhooks"]],
  },
  {
    name: "useEnableWebhook invalidates list + detail",
    hook: () => useEnableWebhook(),
    input: "wh-1",
    expectedKeys: [["user", "webhooks"], ["user", "webhooks", "wh-1"]],
  },
  {
    name: "useDisableWebhook invalidates list + detail",
    hook: () => useDisableWebhook(),
    input: "wh-1",
    expectedKeys: [["user", "webhooks"], ["user", "webhooks", "wh-1"]],
  },
  {
    name: "useDeleteFederation invalidates federations list + by-target",
    hook: () => useDeleteFederation(),
    input: "fed-1",
    expectedKeys: [["user", "federations"], ["federation-by-target"]],
  },
  {
    name: "useFailoverFederation invalidates federations + detail + by-target",
    hook: () => useFailoverFederation(),
    input: {
      id: "fed-1",
      body: { newPrimaryRegionId: "r1", newPrimaryBucket: "b" },
    },
    expectedKeys: [
      ["user", "federations"],
      ["user", "federations", "fed-1"],
      ["federation-by-target"],
    ],
  },
  {
    name: "useResyncFederation invalidates per-federation detail",
    hook: () => useResyncFederation(),
    input: "fed-1",
    expectedKeys: [["user", "federations", "fed-1"]],
  },
  {
    name: "useRevokeInvite invalidates [admin,invites]",
    hook: () => useRevokeInvite(),
    input: "inv-1",
    expectedKeys: [["admin", "invites"]],
  },
  {
    name: "useCreateInvite invalidates [admin,invites]",
    hook: () => useCreateInvite(),
    input: { label: "test", ttlHours: 24 },
    expectedKeys: [["admin", "invites"]],
  },
  {
    name: "usePutBucketVersioning invalidates versioning + object-lock",
    hook: () => usePutBucketVersioning("r1", "b1"),
    input: { status: "enabled" },
    expectedKeys: [
      ["user", "regions", "r1", "buckets", "b1", "versioning"],
      ["user", "regions", "r1", "buckets", "b1", "object-lock"],
    ],
  },
  {
    name: "usePutBucketEncryption invalidates encryption read",
    hook: () => usePutBucketEncryption("r1", "b1"),
    input: { algorithm: "AES256" },
    expectedKeys: [["user", "regions", "r1", "buckets", "b1", "encryption"]],
  },
  {
    name: "useDeleteBucketEncryption invalidates encryption read",
    hook: () => useDeleteBucketEncryption("r1", "b1"),
    input: undefined,
    expectedKeys: [["user", "regions", "r1", "buckets", "b1", "encryption"]],
  },
  {
    name: "useUpsertRole invalidates [admin,policies]",
    hook: () => useUpsertRole(),
    input: { id: "r1", label: "Role 1", description: "", capabilities: [] },
    expectedKeys: [["admin", "policies"]],
  },
  {
    name: "useDeleteRole invalidates [admin,policies]",
    hook: () => useDeleteRole(),
    input: "r1",
    expectedKeys: [["admin", "policies"]],
  },
];

describe("v1.11.0.14 mutation invalidation audit", () => {
  for (const c of cases) {
    it(c.name, async () => {
      if (c.setup) {
        c.setup();
      } else {
        stubFetch(200, {});
      }

      const qc = freshClient();
      const spy = vi.spyOn(qc, "invalidateQueries");

      const { result } = renderHook(() => c.hook(), {
        wrapper: withProviders(qc),
      });

      await result.current.mutateAsync(c.input as never);

      // onSuccess fires synchronously after mutateAsync resolves but the
      // invalidate call runs through the QueryClient microtask queue,
      // so wait until the spy has at least the expected count.
      await waitFor(() => {
        expect(spy.mock.calls.length).toBeGreaterThanOrEqual(
          c.expectedKeys.length,
        );
      });

      // Collect every queryKey that was invalidated and assert each
      // expected key is in the set.
      const invalidatedKeys = spy.mock.calls.map((call) => call[0]?.queryKey);
      for (const expected of c.expectedKeys) {
        const found = invalidatedKeys.some(
          (k) => JSON.stringify(k) === JSON.stringify(expected),
        );
        expect(
          found,
          `expected invalidateQueries({queryKey: ${JSON.stringify(expected)}}), got ${JSON.stringify(invalidatedKeys)}`,
        ).toBe(true);
      }
    });
  }
});

// Hooks routed through openapi-fetch (client.POST/PUT/DELETE) need
// their own setup — the mutation never touches global.fetch.
describe("v1.11.0.14 openapi-fetch mutation invalidation audit", () => {
  type ClientCase = {
    name: string;
    hook: () => { mutateAsync: (...args: any[]) => Promise<any> };
    input?: unknown;
    expectedKeys: string[][];
    method: "POST" | "PUT" | "DELETE";
    response?: unknown;
  };

  const clientCases: ClientCase[] = [
    {
      name: "useDeleteUserSync invalidates list",
      hook: () => useDeleteUserSync(),
      input: "sync-1",
      expectedKeys: [["user", "syncs"]],
      method: "DELETE",
    },
    {
      name: "useDeleteServiceAccount invalidates list",
      hook: () => useDeleteServiceAccount(),
      input: "sa-1",
      expectedKeys: [["admin", "service-accounts"]],
      method: "DELETE",
      // useDeleteServiceAccount uses bare fetch, not the client. Skip
      // method mock — overridden in setup.
    },
    {
      name: "useRotateServiceAccount invalidates list + detail",
      hook: () => useRotateServiceAccount(),
      input: "sa-1",
      expectedKeys: [
        ["admin", "service-accounts"],
        ["admin", "service-accounts", "sa-1"],
      ],
      method: "POST",
    },
    {
      name: "useDeleteUserBackup invalidates list",
      hook: () => useDeleteUserBackup(),
      input: "bk-1",
      expectedKeys: [["user", "backups"]],
      method: "DELETE",
      response: undefined,
    },
    {
      name: "useRunUserBackup invalidates list + detail",
      hook: () => useRunUserBackup(),
      input: "bk-1",
      expectedKeys: [["user", "backups"], ["user", "backups", "bk-1"]],
      method: "POST",
      response: { id: "bk-1", status: "queued" },
    },
    {
      name: "useUpdateUserBackup invalidates list + detail",
      hook: () => useUpdateUserBackup(),
      input: { id: "bk-1", body: {} },
      expectedKeys: [["user", "backups"], ["user", "backups", "bk-1"]],
      method: "PUT",
      response: { id: "bk-1" },
    },
  ];

  for (const c of clientCases) {
    it(c.name, async () => {
      const ok = true;
      const response = { ok, status: 200 } as Response;
      const data = c.response ?? {};
      // The service-accounts hooks go through bare fetch; the rest go
      // through openapi-fetch. Stub both to be safe.
      stubFetch(200, data);
      vi.mocked(client[c.method]).mockResolvedValue({
        data,
        error: undefined,
        response,
      } as never);

      const qc = freshClient();
      const spy = vi.spyOn(qc, "invalidateQueries");

      const { result } = renderHook(() => c.hook(), {
        wrapper: withProviders(qc),
      });

      await result.current.mutateAsync(c.input as never);

      await waitFor(() => {
        expect(spy.mock.calls.length).toBeGreaterThanOrEqual(
          c.expectedKeys.length,
        );
      });

      const invalidatedKeys = spy.mock.calls.map((call) => call[0]?.queryKey);
      for (const expected of c.expectedKeys) {
        const found = invalidatedKeys.some(
          (k) => JSON.stringify(k) === JSON.stringify(expected),
        );
        expect(
          found,
          `expected invalidateQueries({queryKey: ${JSON.stringify(expected)}}), got ${JSON.stringify(invalidatedKeys)}`,
        ).toBe(true);
      }
    });
  }
});
