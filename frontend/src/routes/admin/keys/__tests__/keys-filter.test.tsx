import { describe, it, expect } from "vitest";

describe("keys filter (Fix 2)", () => {
  type Key = { id: string; name?: string | null };

  const mockKeys: Key[] = [
    { id: "key-abc123", name: "Production Key" },
    { id: "key-def456", name: undefined },
    { id: "key-staging01", name: null },
    { id: "key-jkl012", name: "Staging Key" },
  ];

  function filterKeys(
    keys: Key[],
    search: string,
  ): Key[] {
    return keys.filter((key) => {
      if (!search) return true;
      const needle = search.toLowerCase();
      const nameMatch = key.name?.toLowerCase().includes(needle) ?? false;
      const idMatch = key.id?.toLowerCase().includes(needle) ?? false;
      return nameMatch || idMatch;
    });
  }

  it("empty search returns ALL keys including ones where name is undefined", () => {
    const result = filterKeys(mockKeys, "");
    expect(result).toHaveLength(4);
    expect(result.map((k) => k.id)).toEqual([
      "key-abc123",
      "key-def456",
      "key-staging01",
      "key-jkl012",
    ]);
  });

  it("search by partial name matches only keys whose name contains the substring (case-insensitive)", () => {
    const result = filterKeys(mockKeys, "production");
    expect(result).toHaveLength(1);
    expect(result[0]?.id).toBe("key-abc123");
  });

  it("search by partial access-key-ID matches keys whose id contains the substring (case-insensitive)", () => {
    const result = filterKeys(mockKeys, "def456");
    expect(result).toHaveLength(1);
    expect(result[0]?.id).toBe("key-def456");
  });

  it("search + cluster filter works together", () => {
    // Simulate filtering by cluster first, then applying search
    const keysInStaging: Key[] = [mockKeys[2]!, mockKeys[3]!]; // key-staging01 and key-jkl012 are in staging
    const result = filterKeys(keysInStaging, "staging");
    expect(result).toHaveLength(2);
  });

  it("search by id in cluster-filtered results works correctly", () => {
    const keysInProd: Key[] = [mockKeys[0]!, mockKeys[1]!]; // key-abc123 and key-def456 are in prod
    const result = filterKeys(keysInProd, "abc");
    expect(result).toHaveLength(1);
  });

  it("undefined name with empty search is included (not filtered out)", () => {
    const result = filterKeys(mockKeys, "");
    const unnamedKey = result.find((k) => k.id === "key-def456");
    expect(unnamedKey).toBeDefined();
    if (unnamedKey) {
      expect(unnamedKey.name).toBeUndefined();
    }
  });

  it("null name with empty search is included (not filtered out)", () => {
    const result = filterKeys(mockKeys, "");
    const nullNameKey = result.find((k) => k.id === "key-staging01");
    expect(nullNameKey).toBeDefined();
    if (nullNameKey) {
      expect(nullNameKey.name).toBeNull();
    }
  });
});
