"use client";

import { useMemo } from "react";
import {
  AlertTriangle,
  Calendar,
  CheckCircle2,
  Clock,
  Gauge,
  RotateCw,
  Sparkles,
  Zap,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type {
  AgentRuntime,
  CapacityTier,
  RuntimeWindowMetrics,
} from "@multica/core/types";
import { runtimeCapacityOptions } from "@multica/core/runtimes/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useTimeAgo } from "../../i18n";
import { ProviderLogo } from "./provider-logo";

const TIER_CONFIG: Record<
  CapacityTier,
  { label: string; badgeClass: string; barClass: string; icon: typeof Zap }
> = {
  AMPLE: {
    label: "Ample Capacity",
    badgeClass:
      "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20",
    barClass: "bg-emerald-500",
    icon: CheckCircle2,
  },
  LOW: {
    label: "Low Capacity",
    badgeClass:
      "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20",
    barClass: "bg-amber-500",
    icon: AlertTriangle,
  },
  CRITICAL: {
    label: "Critical Quota",
    badgeClass:
      "bg-orange-500/15 text-orange-600 dark:text-orange-400 border-orange-500/30",
    barClass: "bg-orange-500",
    icon: AlertTriangle,
  },
  EXHAUSTED: {
    label: "Quota Exhausted",
    badgeClass:
      "bg-destructive/15 text-destructive dark:text-destructive-foreground border-destructive/30",
    barClass: "bg-destructive",
    icon: AlertTriangle,
  },
  UNKNOWN: {
    label: "Unknown",
    badgeClass: "bg-muted text-muted-foreground border-border",
    barClass: "bg-muted-foreground",
    icon: Gauge,
  },
};

function HorizonMeter({
  metrics,
  icon: Icon,
}: {
  metrics: RuntimeWindowMetrics;
  icon: typeof Clock | typeof Calendar;
}) {
  const tier = metrics.tier ?? "UNKNOWN";
  const config = TIER_CONFIG[tier] ?? TIER_CONFIG.UNKNOWN;
  const remainingClamped = Math.max(0, Math.min(100, metrics.remaining_percent));

  return (
    <div className="flex flex-col gap-2 rounded-lg border bg-muted/20 p-3.5 transition-colors hover:bg-muted/30">
      <div className="flex items-center justify-between text-body">
        <span className="font-medium text-foreground">{metrics.label}</span>
        <span className="font-mono text-body font-semibold tabular-nums text-foreground">
          {metrics.remaining_percent.toFixed(1)}%{" "}
          <span className="text-caption font-normal text-muted-foreground">
            left
          </span>
        </span>
      </div>

      {/* Progress Track */}
      <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
        <div
          className={cn("h-full transition-all duration-500", config.barClass)}
          style={{ width: `${remainingClamped}%` }}
        />
      </div>

      <div className="flex items-center justify-between text-caption text-muted-foreground">
        <span className="inline-flex items-center gap-1">
          <Icon className="h-3 w-3" />
          {metrics.time_until_reset
            ? `Resets in ${metrics.time_until_reset}`
            : "Window active"}
        </span>
        <span className="text-micro font-medium uppercase tracking-wider">
          {config.label}
        </span>
      </div>
    </div>
  );
}

export function RuntimeCapacityCard({ runtime }: { runtime: AgentRuntime }) {
  const timeAgo = useTimeAgo();
  const { data, isLoading, isFetching, refetch } = useQuery(
    runtimeCapacityOptions(runtime.id),
  );

  const tier = data?.effective_tier ?? "UNKNOWN";
  const tierConfig = TIER_CONFIG[tier] ?? TIER_CONFIG.UNKNOWN;
  const TierIcon = tierConfig.icon;

  const isSupported = data?.supported !== false && !data?.error;
  const checkedAgo = data?.checked_at ? timeAgo(data.checked_at) : null;

  return (
    <div className="rounded-lg border bg-card p-5 shadow-xs">
      {/* Top Header */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/10 text-primary">
            <Gauge className="h-4 w-4" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h3 className="text-body font-semibold text-foreground">
                Runtime Capacity & Rate Limits
              </h3>
              <span className="inline-flex items-center gap-1 rounded-md border bg-muted/40 px-2 py-0.5 text-micro font-medium text-muted-foreground">
                <ProviderLogo provider={runtime.provider} className="h-3 w-3" />
                <span className="capitalize">{runtime.provider}</span>
              </span>
            </div>
            <p className="text-caption text-muted-foreground">
              Live retrieval for 5-hour session and 7-day weekly quota windows
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {isSupported && (
            <Badge
              variant="outline"
              className={cn("gap-1 py-1 font-medium", tierConfig.badgeClass)}
            >
              <TierIcon className="h-3.5 w-3.5" />
              <span>{tierConfig.label}</span>
            </Badge>
          )}

          <Tooltip>
            <TooltipTrigger
              onClick={() => void refetch()}
              disabled={isFetching}
              aria-label="Refresh quota"
              className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background hover:bg-muted text-foreground transition-colors disabled:opacity-50"
            >
              <RotateCw
                className={cn("h-3.5 w-3.5", isFetching && "animate-spin")}
              />
            </TooltipTrigger>
            <TooltipContent>Refresh live capacity</TooltipContent>
          </Tooltip>
        </div>
      </div>

      {/* Main Content Area */}
      <div className="pt-4">
        {isLoading ? (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <Skeleton className="h-24 w-full rounded-lg" />
            <Skeleton className="h-24 w-full rounded-lg" />
          </div>
        ) : !isSupported ? (
          <div className="flex items-start gap-3 rounded-lg border border-dashed p-4 text-muted-foreground">
            <Sparkles className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground/70" />
            <div className="text-caption">
              <p className="font-medium text-foreground">
                Live Quota Engine Ready
              </p>
              <p className="mt-0.5">
                Live rate limit probing is enabled for{" "}
                <span className="font-medium text-foreground">AntiGravity</span>,{" "}
                <span className="font-medium text-foreground">Claude Code</span>, and{" "}
                <span className="font-medium text-foreground">Codex</span> runtimes.
                {data?.error ? (
                  <span className="mt-1 block text-muted-foreground">
                    Probe status: {data.error}
                  </span>
                ) : (
                  <span className="mt-1 block">
                    No active external rate-limit session reported for this provider yet.
                  </span>
                )}
              </p>
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            {/* Horizon Meters Grid */}
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              {data?.session_5h ? (
                <HorizonMeter metrics={data.session_5h} icon={Clock} />
              ) : (
                <div className="rounded-lg border bg-muted/10 p-3.5 text-caption text-muted-foreground">
                  5-Hour Session: Unlimited / No cap reported
                </div>
              )}

              {data?.weekly_7d ? (
                <HorizonMeter metrics={data.weekly_7d} icon={Calendar} />
              ) : (
                <div className="rounded-lg border bg-muted/10 p-3.5 text-caption text-muted-foreground">
                  Weekly Limit: Unlimited / No cap reported
                </div>
              )}
            </div>

            {/* Extra Metrics (Reset Credits or Per-Model buckets) */}
            {(data?.reset_credits != null ||
              (data?.model_buckets && Object.keys(data.model_buckets).length > 0)) && (
              <div className="mt-3 flex flex-wrap items-center gap-2 rounded-md bg-muted/30 px-3 py-2 text-caption">
                {data.reset_credits != null && (
                  <span className="inline-flex items-center gap-1 font-medium text-foreground">
                    <Zap className="h-3 w-3 text-amber-500" />
                    Reset Credits:{" "}
                    <span className="font-mono">{data.reset_credits}</span>
                  </span>
                )}

                {data.model_buckets &&
                  Object.entries(data.model_buckets).map(([k, m]) => (
                    <span
                      key={k}
                      className="inline-flex items-center gap-1 rounded bg-muted/60 px-1.5 py-0.5 text-micro font-medium text-muted-foreground"
                    >
                      <span className="capitalize">{k}</span>:{" "}
                      <span className="font-mono text-foreground">
                        {m.remaining_percent.toFixed(0)}%
                      </span>
                    </span>
                  ))}
              </div>
            )}

            {/* Footer Metadata */}
            <div className="mt-3 flex flex-wrap items-center justify-between text-micro text-muted-foreground">
              <span>
                {checkedAgo ? `Checked ${checkedAgo}` : "Live data"}
              </span>
              <span>
                Cache-invariant turn injection active (MUL-5377)
              </span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
