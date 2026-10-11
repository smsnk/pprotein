// /api/analysis の JSON の型（internal/analysis の Report / RunsResponse / Diff）。
// キーの単位: …Sec は秒、RateFromPrev / Share は 0〜1 の割合、S499 は 499 の件数。

export interface ScoreInfo {
  Value: number;
  Passed: boolean;
}

export interface Commit {
  Hash: string;
  Message: string;
  Ref?: string;
}

export interface ConfigHash {
  Analysis: string;
  DBQuery: string;
}

export type Role = "score" | "driver" | "penalty" | "";

export interface SeriesStat {
  Name: string;
  Role?: Role;
  Query?: string;
  Agg: string;
  Total: number;
  T50Sec?: number;
  T90Sec?: number;
}

export interface Quantiles {
  P50: number;
  P90: number;
  P99: number;
  Max: number;
}

export interface StepResult {
  Name: string;
  Reached: number;
  RateFromPrev: number;
  StoppedHere: number;
  PerEntity: number;
  FromPrevSec?: Quantiles;
}

export interface LifecycleResult {
  Name: string;
  Entities: number;
  Steps: StepResult[];
  Error?: string;
}

export interface BreakdownResult {
  Name: string;
  Columns: string[] | null;
  Rows: string[][] | null;
  Share?: number[];
  TotalRows: number;
  Error?: string;
}

export interface Timeline {
  OffsetSec: number[];
  Requests: number[];
  S2xx: number[];
  S3xx: number[];
  S4xx: number[];
  S5xx: number[];
  S499: number[];
  P50Sec: number[];
  P99Sec: number[];
  Series?: { Name: string; Values: (number | null)[] }[];
  Endpoints?: { Name: string; Values: number[] }[];
}

export interface Report {
  GroupId: string;
  Origin: string;
  BucketSec: number;
  Warnings?: string[];
  Score?: ScoreInfo;
  Commit?: Commit;
  ConfigHash: ConfigHash;
  Totals: {
    Requests: number;
    S2xx: number;
    S4xx: number;
    S5xx: number;
    S499: number;
    P50Sec: number;
    P99Sec: number;
    PeakRequests: number;
    PeakOffsetSec: number;
    Endpoints?: { [key: string]: number };
  };
  SeriesStats: SeriesStat[] | null;
  Timeline: Timeline;
  Lifecycles: LifecycleResult[] | null;
  Breakdowns: BreakdownResult[] | null;
}

export interface RunSummary {
  GroupId: string;
  Score?: ScoreInfo;
  Commit?: Commit;
  ConfigHash: ConfigHash;
  Warnings?: string[];
  Requests: {
    Requests: number;
    S4xx: number;
    S5xx: number;
    S499: number;
    P50Sec: number;
    P99Sec: number;
    PeakRequests: number;
  };
  Series: SeriesStat[] | null;
  Lifecycles:
    | {
        Name: string;
        Entities: number;
        Steps: { Name: string; Reached: number }[];
      }[]
    | null;
  TopEndpoints: { Key: string; Count: number; Share: number }[] | null;
}

export interface Diff {
  Before: string;
  After: string;
  Score?: { Before?: ScoreInfo; After?: ScoreInfo };
  Warnings?: string[];
  Series:
    | {
        Name: string;
        Role?: Role;
        Before?: number;
        After?: number;
        T50SecBefore?: number;
        T50SecAfter?: number;
        T90SecBefore?: number;
        T90SecAfter?: number;
      }[]
    | null;
  Requests: { Name: string; Before: number; After: number }[];
  Endpoints:
    | {
        Key: string;
        CountBefore: number;
        CountAfter: number;
        ShareBefore: number;
        ShareAfter: number;
      }[]
    | null;
  Lifecycles:
    | {
        Name: string;
        EntitiesBefore?: number;
        EntitiesAfter?: number;
        Steps: { Name: string; Before?: StepResult; After?: StepResult }[];
      }[]
    | null;
  Breakdowns:
    | {
        Name: string;
        Keyed: boolean;
        RowsBefore?: number;
        RowsAfter?: number;
        Top1Before: number;
        Top1After: number;
        Top3Before: number;
        Top3After: number;
        NewInTop?: string[];
        Rows: { Key: string; Before?: number; After?: number }[] | null;
      }[]
    | null;
}

export async function getJSON<T>(url: string): Promise<T> {
  const resp = await fetch(url);
  if (!resp.ok) {
    throw new Error(
      `${url}: ${resp.status} ${(await resp.text()).slice(0, 200)}`,
    );
  }
  return (await resp.json()) as T;
}

export const ROLES: Role[] = ["score", "driver", "penalty"];

export const ROLE_LABEL: { [key: string]: string } = {
  score: "得点の項 (score)",
  driver: "得点を生む母数 (driver)",
  penalty: "得点を減らすもの (penalty)",
};

export function num(v: number | undefined | null): string {
  if (v === undefined || v === null) return "-";
  if (Number.isInteger(v)) return v.toLocaleString();
  return v.toFixed(Math.abs(v) < 10 ? 3 : 1);
}

export function pct(v: number | undefined | null): string {
  if (v === undefined || v === null) return "-";
  return `${(v * 100).toFixed(1)}%`;
}

export function sec(v: number | undefined | null): string {
  if (v === undefined || v === null) return "-";
  return v >= 1 ? `${v.toFixed(2)}s` : `${(v * 1000).toFixed(0)}ms`;
}

// change は after - before の向きと大きさを返す。higherIsBetter が false の指標（エラー・遅延）は向きを逆にする。
export function change(
  before: number | undefined,
  after: number | undefined,
  higherIsBetter = true,
) {
  if (before === undefined || after === undefined) {
    return { text: before === undefined ? "追加" : "削除", cls: "" };
  }
  const d = after - before;
  const rel =
    before !== 0
      ? ` (${d >= 0 ? "+" : ""}${((d / Math.abs(before)) * 100).toFixed(1)}%)`
      : "";
  const sign = d > 0 ? "+" : "";
  const good = higherIsBetter ? d > 0 : d < 0;
  return {
    text: `${sign}${num(d)}${rel}`,
    cls: d === 0 ? "" : good ? "better" : "worse",
  };
}

export function shortHash(c?: Commit): string {
  return c ? c.Hash.substring(0, 7) : "";
}
