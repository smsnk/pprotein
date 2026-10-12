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
  EndpointStats?: EndpointStat[];
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
  Endpoints?: EndpointStat[];
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

// --- セッションの導線（/api/analysis/:id/sessions, /transitions） ---

export interface SessionSummary {
  ID: string;
  Hosts?: string[];
  StartSec: number;
  EndSec: number;
  Requests: number;
  Errors: number;
  S499: number;
  MaxReqSec: number;
  Lifecycles: { Name: string; Reached: number; LastStep?: string }[] | null;
}

export interface SessionsResponse {
  GroupId: string;
  Lifecycles: { Name: string; Steps: string[] }[] | null;
  Total: number;
  Sessions: SessionSummary[];
}

export interface SessionRequest {
  OffsetSec: number;
  Method: string;
  Endpoint: string;
  URI: string;
  Host?: string;
  Status: number;
  ReqTimeSec: number;
  Steps?: string[];
}

export interface SessionDetail {
  GroupId: string;
  Summary: SessionSummary;
  Requests: SessionRequest[];
}

export interface TransitionsResponse {
  GroupId: string;
  Lifecycle?: string;
  Sessions: number;
  Transitions: {
    From: string;
    To: string;
    Count: number;
    ShareOfFrom: number;
    GapP50Sec: number;
  }[];
}

// statusColor はステータスの色（2xx 青 / 3xx 灰 / 4xx 橙 / 499 紫 / 5xx 赤）。
export function statusColor(status: number): string {
  if (status === 499) return "#8e44ad";
  if (status >= 500) return "#c0392b";
  if (status >= 400) return "#e67e22";
  if (status >= 300) return "#7f8c8d";
  return "#2c6fbb";
}

// prettyEndpoint は alp.yml の matching_groups（正規表現）を読みやすいパスに直す。
// 例: "GET ^/api/livestream/[0-9]+/livecomment$" → "GET /api/livestream/:id/livecomment"
export function prettyEndpoint(s: string): string {
  // 文字クラスを先に置き換える（先にアンカーの ^ を消すと [^/]+ の ^ まで消える）
  return s
    .replace(/\[0-9\]\+/g, ":id")
    .replace(/\[\^\/\]\+/g, ":name")
    .replace(/\[[^\]]*\][+*]/g, ":x")
    .replace(/\.\+/g, ":x")
    .replace(/\^|\$/g, "");
}

// --- エンドポイント別（/api/analysis/:id/endpoints、Report.EndpointStats） ---

export interface EndpointStat {
  Key: string;
  Count: number;
  Share: number;
  P50Sec: number;
  P99Sec: number;
  SumSec: number;
  Errors: number;
  S499: number;
}

export interface EndpointTimeline {
  GroupId: string;
  BucketSec: number;
  OffsetSec: number[];
  Endpoints: {
    Key: string;
    Count: number[];
    P50Sec: number[];
    P99Sec: number[];
    Errors: number[];
    S499: number[];
  }[];
}
