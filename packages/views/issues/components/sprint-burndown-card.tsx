"use client";

import { useEffect, useId, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { sprintBurndownOptions } from "@multica/core/sprints";
import type { Sprint, SprintBurndownPoint } from "@multica/core/types";

export interface SprintBurndownCardProps {
  sprint: Sprint;
  className?: string;
}

export function SprintBurndownCard({ sprint, className }: SprintBurndownCardProps) {
  const wsId = useWorkspaceId();
  const titleId = useId();
  const descId = useId();

  const [prefersReducedMotion, setPrefersReducedMotion] = useState(false);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    setPrefersReducedMotion(mediaQuery.matches);
    const handler = (e: MediaQueryListEvent) => setPrefersReducedMotion(e.matches);
    mediaQuery.addEventListener("change", handler);
    return () => mediaQuery.removeEventListener("change", handler);
  }, []);

  const { data: burndownPoints = [], isLoading, isError } = useQuery(
    sprintBurndownOptions(wsId, sprint.id),
  );

  const points: SprintBurndownPoint[] = useMemo(() => {
    if (burndownPoints.length > 0) return burndownPoints;
    // Fallback point from sprint metadata if server returns empty points
    const total = sprint.total_count ?? 0;
    const done = sprint.done_count ?? 0;
    const remaining = Math.max(0, total - done);
    const startDate = sprint.start_date || new Date().toISOString().slice(0, 10);
    const endDate = sprint.end_date || new Date().toISOString().slice(0, 10);
    return [
      { date: startDate, remaining: total, ideal: total, completed: 0 },
      { date: endDate, remaining, ideal: 0, completed: done },
    ];
  }, [burndownPoints, sprint]);

  const totalIssues = sprint.total_count ?? (points[0]?.remaining ?? 0);
  const doneIssues = sprint.done_count ?? (points[points.length - 1]?.completed ?? 0);
  const currentRemaining = Math.max(0, totalIssues - doneIssues);

  // SVG dimensions & scales
  const width = 480;
  const height = 180;
  const paddingLeft = 36;
  const paddingRight = 16;
  const paddingTop = 20;
  const paddingBottom = 28;

  const chartWidth = width - paddingLeft - paddingRight;
  const chartHeight = height - paddingTop - paddingBottom;

  const maxY = Math.max(
    1,
    ...points.map((p) => Math.max(p.remaining, p.ideal, p.completed, totalIssues)),
  );

  const getX = (index: number) => {
    if (points.length <= 1) return paddingLeft + chartWidth / 2;
    return paddingLeft + (index / (points.length - 1)) * chartWidth;
  };

  const getY = (val: number) => {
    return paddingTop + chartHeight - (val / maxY) * chartHeight;
  };

  const idealPath = useMemo(() => {
    if (points.length === 0) return "";
    return points
      .map((p, i) => `${i === 0 ? "M" : "L"} ${getX(i).toFixed(1)} ${getY(p.ideal).toFixed(1)}`)
      .join(" ");
  }, [points, chartWidth, chartHeight, maxY]);

  const remainingPath = useMemo(() => {
    if (points.length === 0) return "";
    return points
      .map((p, i) => `${i === 0 ? "M" : "L"} ${getX(i).toFixed(1)} ${getY(p.remaining).toFixed(1)}`)
      .join(" ");
  }, [points, chartWidth, chartHeight, maxY]);

  const completedPath = useMemo(() => {
    if (points.length === 0) return "";
    return points
      .map((p, i) => `${i === 0 ? "M" : "L"} ${getX(i).toFixed(1)} ${getY(p.completed).toFixed(1)}`)
      .join(" ");
  }, [points, chartWidth, chartHeight, maxY]);

  const gridYLines = [0, 0.5, 1];

  return (
    <div
      className={`rounded-xl border border-border bg-card p-4 shadow-xs text-card-foreground ${className ?? ""}`}
    >
      <div className="flex items-center justify-between gap-3 pb-3 border-b border-border/60">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h3 id={titleId} className="text-sm font-semibold tracking-tight text-pretty truncate">
              {sprint.name}
            </h3>
            <span
              className={`inline-flex items-center px-1.5 py-0.5 text-[11px] font-medium rounded-md uppercase tracking-wider ${
                sprint.status === "active"
                  ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                  : sprint.status === "completed"
                    ? "bg-blue-500/15 text-blue-600 dark:text-blue-400"
                    : "bg-muted text-muted-foreground"
              }`}
            >
              {sprint.status}
            </span>
          </div>
          {sprint.goal && (
            <p id={descId} className="text-xs text-muted-foreground mt-0.5 truncate text-pretty">
              {sprint.goal}
            </p>
          )}
        </div>

        <div className="flex items-center gap-4 text-xs font-mono tabular-nums shrink-0">
          <div>
            <span className="text-muted-foreground">Total: </span>
            <span className="font-semibold text-foreground">{totalIssues}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Done: </span>
            <span className="font-semibold text-emerald-600 dark:text-emerald-400">{doneIssues}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Remaining: </span>
            <span className="font-semibold text-foreground">{currentRemaining}</span>
          </div>
        </div>
      </div>

      {isLoading ? (
        <div
          className="flex h-44 items-center justify-center text-xs text-muted-foreground"
          aria-live="polite"
        >
          Loading…
        </div>
      ) : isError ? (
        <div
          className="flex h-44 items-center justify-center text-xs text-destructive"
          aria-live="polite"
        >
          Failed to load burndown data.
        </div>
      ) : (
        <div className="mt-3">
          <svg
            role="img"
            aria-label={`Sprint burndown: ${currentRemaining} issues remaining`}
            aria-labelledby={sprint.goal ? `${titleId} ${descId}` : titleId}
            viewBox={`0 0 ${width} ${height}`}
            className="w-full h-auto overflow-visible select-none"
          >
            {/* Background grid lines */}
            {gridYLines.map((ratio, index) => {
              const y = paddingTop + chartHeight * (1 - ratio);
              const val = Math.round(maxY * ratio);
              return (
                <g key={index} aria-hidden="true">
                  <line
                    x1={paddingLeft}
                    y1={y}
                    x2={width - paddingRight}
                    y2={y}
                    stroke="currentColor"
                    strokeOpacity={0.1}
                    strokeDasharray="2 3"
                    className="text-foreground"
                  />
                  <text
                    x={paddingLeft - 6}
                    y={y + 3}
                    textAnchor="end"
                    className="fill-muted-foreground text-[10px] font-mono tabular-nums"
                  >
                    {val}
                  </text>
                </g>
              );
            })}

            {/* X-axis date labels */}
            {points.map((p, i) => {
              // Show first, middle (if >2), and last dates
              if (i === 0 || i === points.length - 1 || (points.length >= 4 && i === Math.floor(points.length / 2))) {
                const x = getX(i);
                const displayDate = p.date.length >= 10 ? p.date.slice(5) : p.date;
                return (
                  <text
                    key={i}
                    x={x}
                    y={height - 8}
                    textAnchor={i === 0 ? "start" : i === points.length - 1 ? "end" : "middle"}
                    className="fill-muted-foreground text-[10px] font-mono tabular-nums"
                    aria-hidden="true"
                  >
                    {displayDate}
                  </text>
                );
              }
              return null;
            })}

            {/* Ideal Guideline */}
            {idealPath && (
              <path
                d={idealPath}
                fill="none"
                stroke="currentColor"
                strokeOpacity={0.35}
                strokeWidth={1.5}
                strokeDasharray="4 4"
                className="text-muted-foreground"
              />
            )}

            {/* Completed Line */}
            {completedPath && (
              <path
                d={completedPath}
                fill="none"
                stroke="#10b981"
                strokeWidth={2}
                strokeLinecap="round"
                strokeLinejoin="round"
                className={prefersReducedMotion ? "" : "transition-[stroke-dashoffset,opacity] duration-300"}
              />
            )}

            {/* Remaining Line */}
            {remainingPath && (
              <path
                d={remainingPath}
                fill="none"
                stroke="#3b82f6"
                strokeWidth={2.5}
                strokeLinecap="round"
                strokeLinejoin="round"
                className={prefersReducedMotion ? "" : "transition-[stroke-dashoffset,opacity] duration-300"}
              />
            )}

            {/* Data Dots for remaining */}
            {points.map((p, i) => (
              <circle
                key={i}
                cx={getX(i)}
                cy={getY(p.remaining)}
                r={3}
                fill="#3b82f6"
                className={prefersReducedMotion ? "" : "transition-transform hover:scale-125"}
                aria-hidden="true"
              />
            ))}
          </svg>

          {/* Legend */}
          <div className="flex items-center justify-end gap-4 mt-2 pt-2 border-t border-border/40 text-[11px] text-muted-foreground">
            <div className="flex items-center gap-1.5">
              <span className="w-3 h-0.5 border-b border-dashed border-muted-foreground/60" aria-hidden="true" />
              <span>Ideal</span>
            </div>
            <div className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-emerald-500" aria-hidden="true" />
              <span>Completed</span>
            </div>
            <div className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-blue-500" aria-hidden="true" />
              <span>Remaining</span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
