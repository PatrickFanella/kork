import { useEffect, useId, useState } from "react";
import { api, errorMessage, type NameCount, type Stats } from "../../api";
import { Notice, PageLoading } from "../../components/ui";
import { countryLabel, formatPercent, referrerLabel } from "../../lib/stats";
import { fillDays } from "../../lib/validate";
import type { EditorProps } from "./types";
import { ProBadge, UpgradeNote } from "../../components/Pro";
import { usePlan } from "../../plan";
import { WeeklyEmailToggle } from "./WeeklyEmailToggle";

// Ranges past the plan's stats_days are shown disabled with a Pro badge.
const RANGES = [7, 30, 90, 365] as const;

export function StatsTab({ page }: EditorProps) {
  const [days, setDays] = useState<number>(30);
  const [stats, setStats] = useState<{ days: number; data: Stats } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const { plan } = usePlan();
  const maxDays = stats?.data.max_days ?? plan?.stats_days ?? 30;

  useEffect(() => {
    let cancelled = false;
    setError(null);
    api
      .stats(page.id, days)
      .then((data) => !cancelled && setStats({ days: data.days ?? days, data }))
      .catch((err: unknown) => !cancelled && setError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [page.id, days, reload]);

  const locked = RANGES.some((r) => r > maxDays);
  const rangeControl = (
    <div className="stack">
      <div className="segmented" role="group" aria-label="Date range">
        {RANGES.map((r) => (
          <button
            key={r}
            type="button"
            className="segmented-option"
            aria-pressed={days === r}
            disabled={r > maxDays}
            onClick={() => setDays(r)}
          >
            {r} days{r > maxDays && <ProBadge />}
          </button>
        ))}
      </div>
      {locked && <UpgradeNote>Pro keeps {plan?.pro.stats_days ?? 365} days of stats history.</UpgradeNote>}
    </div>
  );

  if (error) {
    return (
      <Notice
        tone="danger"
        title="Couldn't load stats"
        action={
          <button type="button" className="btn btn-sm" onClick={() => setReload((n) => n + 1)}>
            Try again
          </button>
        }
      >
        {error}
      </Notice>
    );
  }
  if (!stats) return <PageLoading label="Loading stats" />;

  const data = stats.data;
  const today = new Date().toISOString().slice(0, 10);
  const lastDay = data.views.reduce((max, v) => (v.day > max ? v.day : max), today);
  const series = fillDays(data.views, stats.days, lastDay);
  const periodViews = series.reduce((sum, v) => sum + v.count, 0);
  const totalClicks = data.blocks.reduce((sum, b) => sum + b.count, 0);
  const blockRows = data.blocks.slice().sort((a, b) => b.count - a.count);

  return (
    <div className="stack-lg">
      {rangeControl}
      <div className="stat-tiles">
        <div className="stat-tile">
          <span className="stat-label">Total views</span>
          <span className="stat-value">{data.total_views.toLocaleString()}</span>
        </div>
        <div className="stat-tile">
          <span className="stat-label">Views, last {stats.days} days</span>
          <span className="stat-value">{periodViews.toLocaleString()}</span>
        </div>
        <div className="stat-tile">
          <span className="stat-label">Clicks, last {stats.days} days</span>
          <span className="stat-value">{totalClicks.toLocaleString()}</span>
        </div>
      </div>

      <section className="card section" aria-labelledby="views-heading">
        <h2 id="views-heading">Daily views</h2>
        <BarChart series={series} />
      </section>

      <section className="card section" aria-labelledby="clicks-heading">
        <h2 id="clicks-heading">Clicks by block</h2>
        {blockRows.length === 0 ? (
          <p className="muted">No clicks yet. Share your page to start collecting them.</p>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Block</th>
                <th scope="col" className="num">
                  Clicks
                </th>
                <th scope="col" className="num">
                  Share
                </th>
                <th scope="col" className="num">
                  <abbr title="Clicks divided by page views">Click-through</abbr>
                </th>
              </tr>
            </thead>
            <tbody>
              {blockRows.map((b) => (
                <tr key={b.block_id}>
                  <td>{b.label || "Deleted block"}</td>
                  <td className="num">{b.count.toLocaleString()}</td>
                  <td className="num">
                    <span className="share-bar" aria-hidden="true">
                      <span style={{ width: `${totalClicks ? (b.count / totalClicks) * 100 : 0}%` }} />
                    </span>
                    {totalClicks ? Math.round((b.count / totalClicks) * 100) : 0}%
                  </td>
                  <td className="num">{b.ctr === undefined ? "–" : formatPercent(b.ctr)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <div className="stats-pair">
        <BreakdownTable
          id="referrers-heading"
          title="Referrers"
          column="Site"
          rows={data.referrers ?? []}
          total={periodViews}
          label={referrerLabel}
          empty="No referrers yet. Sites that link to your page show up here."
        />
        <BreakdownTable
          id="countries-heading"
          title="Countries"
          column="Country"
          rows={data.countries ?? []}
          total={periodViews}
          label={(c) => countryLabel(c)}
          empty="No country data yet."
        />
      </div>
      <p className="muted small">
        Referrers and countries are daily totals per page. No IP addresses, cookies or browser details are stored.
      </p>

      <WeeklyEmailToggle />
    </div>
  );
}

function BreakdownTable({
  id,
  title,
  column,
  rows,
  total,
  label,
  empty,
}: {
  id: string;
  title: string;
  column: string;
  rows: NameCount[];
  /** Page views over the range; shares are of all views, not just the listed rows. */
  total: number;
  label: (name: string) => string;
  empty: string;
}) {
  return (
    <section className="card section" aria-labelledby={id}>
      <h2 id={id}>{title}</h2>
      {rows.length === 0 ? (
        <p className="muted">{empty}</p>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th scope="col">{column}</th>
              <th scope="col" className="num">
                Views
              </th>
              <th scope="col" className="num">
                Share
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.name}>
                <td>{label(r.name)}</td>
                <td className="num">{r.count.toLocaleString()}</td>
                <td className="num">{total ? Math.round((r.count / total) * 100) : 0}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function niceMax(n: number): number {
  if (n <= 4) return 4;
  const mag = 10 ** Math.floor(Math.log10(n));
  const steps = [1, 2, 2.5, 5, 10];
  for (const s of steps) if (s * mag >= n) return s * mag;
  return 10 * mag;
}

function shortDay(day: string): string {
  const d = new Date(`${day}T00:00:00Z`);
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" });
}

function BarChart({ series }: { series: { day: string; count: number }[] }) {
  const titleId = useId();
  const [hover, setHover] = useState<number | null>(null);
  const W = 640;
  const H = 200;
  const pad = { top: 12, right: 8, bottom: 24, left: 32 };
  const innerW = W - pad.left - pad.right;
  const innerH = H - pad.top - pad.bottom;
  const max = niceMax(Math.max(0, ...series.map((s) => s.count)));
  const slot = innerW / Math.max(series.length, 1);
  const barW = Math.max(2, Math.min(24, slot * 0.7));
  const ticks = [0, max / 2, max];
  const labelEvery = Math.ceil(series.length / 6);
  const total = series.reduce((a, b) => a + b.count, 0);
  const peak = series.reduce((best, s) => (s.count > best.count ? s : best), series[0] ?? { day: "", count: 0 });
  const summary =
    total === 0
      ? `No views in the last ${series.length} days.`
      : `${total} views over ${series.length} days. Busiest day ${shortDay(peak.day)} with ${peak.count}.`;
  const hovered = hover !== null ? series[hover] : undefined;

  return (
    <figure className="chart">
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-labelledby={titleId} onMouseLeave={() => setHover(null)}>
        <title id={titleId}>{summary}</title>
        {ticks.map((t) => {
          const y = pad.top + innerH - (t / max) * innerH;
          return (
            <g key={t}>
              <line x1={pad.left} x2={W - pad.right} y1={y} y2={y} className="chart-grid" />
              <text x={pad.left - 6} y={y} className="chart-axis" textAnchor="end" dominantBaseline="middle">
                {Number.isInteger(t) ? t : t.toFixed(1)}
              </text>
            </g>
          );
        })}
        {series.map((s, i) => {
          const h = (s.count / max) * innerH;
          const x = pad.left + i * slot + (slot - barW) / 2;
          return (
            <g key={s.day} onMouseEnter={() => setHover(i)}>
              <rect x={pad.left + i * slot} y={pad.top} width={slot} height={innerH} fill="transparent" />
              <rect
                x={x}
                y={pad.top + innerH - h}
                width={barW}
                height={Math.max(h, s.count > 0 ? 1 : 0)}
                rx={Math.min(3, barW / 3)}
                className={`chart-bar${hover === i ? " is-hover" : ""}`}
              />
              {i % labelEvery === 0 && (
                <text x={pad.left + i * slot + slot / 2} y={H - 6} className="chart-axis" textAnchor="middle">
                  {shortDay(s.day)}
                </text>
              )}
            </g>
          );
        })}
      </svg>
      <figcaption className="chart-caption" aria-live="polite">
        {hovered ? (
          <>
            <strong>{hovered.count}</strong> {hovered.count === 1 ? "view" : "views"} on {shortDay(hovered.day)}
          </>
        ) : (
          summary
        )}
      </figcaption>
    </figure>
  );
}
