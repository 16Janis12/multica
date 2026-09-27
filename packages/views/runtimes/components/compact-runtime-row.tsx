import { cn } from "@multica/ui/lib/utils";
import type { AgentRuntime } from "@multica/core/types";
import { runtimeDisplayName, runtimeCapacityOptions } from "@multica/core/runtimes";
import { useQuery } from "@tanstack/react-query";
import { ProviderLogo } from "./provider-logo";
import { useT } from "../../i18n";

/**
 * One-line runtime row for Step 3's web CLI expand. Provider logo,
 * name + subtitle, online indicator on the right. Selection state is
 * driven by the caller (kept stateless so both StepPlatformFork and
 * any future embedder can share it without duplicating the picker
 * plumbing).
 */
export function CompactRuntimeRow({
  runtime,
  selected,
  onSelect,
  disabled = false,
}: {
  runtime: AgentRuntime;
  selected: boolean;
  onSelect: () => void;
  /** Set while a submit is in flight, so the choice cannot change under it. */
  disabled?: boolean;
}) {
  const { t: tAgents } = useT("agents");
  const online = runtime.status === "online";
  const { data: capacity } = useQuery(runtimeCapacityOptions(runtime.id));
  return (
    // A real button rather than role="button": it brings disabled, focus, and
    // Enter/Space for free. The hand-rolled keydown branch this replaces did
    // not honour disabled at all, so a member could switch runtimes mid-submit
    // and end up with Mika on a machine they no longer had selected.
    <button
      type="button"
      onClick={onSelect}
      disabled={disabled}
      aria-pressed={selected}
      className={cn(
        "flex w-full flex-row items-center gap-3 rounded-lg border bg-card p-4 text-left transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        "disabled:pointer-events-none disabled:opacity-60",
        selected
          ? "border-primary ring-1 ring-primary"
          : "hover:border-foreground/20",
      )}
    >
      <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-body font-medium">
          {runtimeDisplayName(runtime)}
        </div>
        <div className="flex items-center gap-2 text-caption text-muted-foreground">
          <span>{runtime.provider}</span>
          {capacity?.session_5h && (
            <span
              className={cn(
                "rounded px-1.5 py-0.5 text-micro font-medium uppercase",
                capacity.effective_tier === "AMPLE"
                  ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                  : capacity.effective_tier === "LOW"
                    ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
                    : capacity.effective_tier === "CRITICAL"
                      ? "bg-orange-500/15 text-orange-600 dark:text-orange-400"
                      : "bg-destructive/10 text-destructive",
              )}
            >
              {capacity.effective_tier} 5h: {capacity.session_5h.remaining_percent.toFixed(0)}%
            </span>
          )}
        </div>
      </div>
      <span
        className={cn(
          "h-2 w-2 shrink-0 rounded-full",
          online ? "bg-success" : "bg-muted-foreground/40",
        )}
        aria-label={online ? tAgents(($) => $.availability.online) : tAgents(($) => $.availability.offline)}
      />
    </button>
  );
}
