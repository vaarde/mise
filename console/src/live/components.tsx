import { ReactNode, useEffect, useId, useRef, useState } from "react";
import { lifecycleSteps, type LifecycleState, type Tone } from "./model.js";

// ---------------------------------------------------------------------------
// Icons — 16px stroke icons, inline so the console makes no asset requests.

const paths = {
  overview: "M2.5 2.5h4.5v4.5H2.5zM9 2.5h4.5v4.5H9zM2.5 9h4.5v4.5H2.5zM9 9h4.5v4.5H9z",
  changes: "M3 4h10M3 8h6M3 12h8M12 10l2 2-2 2",
  differences: "M5 2v12M11 2v12M2 5h6M8 11h6",
  locations: "M8 14s4.5-4.2 4.5-7.5a4.5 4.5 0 1 0-9 0C3.5 9.8 8 14 8 14zM8 8.2a1.7 1.7 0 1 0 0-3.4 1.7 1.7 0 0 0 0 3.4z",
  check: "M3.5 8.5l3 3 6-7",
  cross: "M4 4l8 8M12 4l-8 8",
  diamond: "M8 2.5L13.5 8 8 13.5 2.5 8z",
  info: "M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12zM8 7.2V11M8 5h.01",
  alert: "M8 2.5l6 11H2zM8 6.5v3.2M8 11.6h.01",
  lock: "M4.5 7V5a3.5 3.5 0 0 1 7 0v2M3.5 7h9v6.5h-9z",
  unlock: "M4.5 7V5a3.5 3.5 0 0 1 6.8-1.2M3.5 7h9v6.5h-9z",
  chevron: "M6 3.5L10.5 8 6 12.5",
  copy: "M5.5 5.5h7v7h-7zM3.5 10.5v-7h7",
  refresh: "M13 8a5 5 0 1 1-1.5-3.6M13 2.5v3h-3",
  shield: "M8 1.8l5 2v4c0 3.2-2.2 5.5-5 6.4-2.8-.9-5-3.2-5-6.4v-4z",
  circle: "M8 13A5 5 0 1 0 8 3a5 5 0 0 0 0 10z",
  arrow: "M3 8h10M9.5 4.5L13 8l-3.5 3.5",
  dash: "M4 8h8",
} as const;

export type IconName = keyof typeof paths;

export function Icon({ name, size = 16, label }: { name: IconName; size?: number; label?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      <path d={paths[name]} />
    </svg>
  );
}

const toneIcon: Record<Tone, IconName> = {
  positive: "check",
  attention: "diamond",
  critical: "alert",
  info: "circle",
  neutral: "dash",
};

/** Status is always glyph + word, never color alone. */
export function Badge({ tone = "neutral", children, icon }: { tone?: Tone; children: ReactNode; icon?: IconName | null }) {
  const glyph = icon === null ? null : icon ?? toneIcon[tone];
  return (
    <span className={`badge ${tone}`}>
      {glyph && <Icon name={glyph} size={12} />}
      {children}
    </span>
  );
}

export function Fingerprint({ hash, label = "Plan fingerprint" }: { hash: string; label?: string }) {
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(hash);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      setExpanded(true);
    }
  }
  return (
    <div className="fingerprint">
      <code className={expanded ? "full" : ""} title={hash} aria-label={`${label} ${hash}`}>
        sha256:{expanded ? hash : `${hash.slice(0, 12)}…${hash.slice(-10)}`}
      </code>
      <button type="button" className="btn link" onClick={() => setExpanded((value) => !value)} aria-expanded={expanded}>
        {expanded ? "Less" : "Full"}
      </button>
      <button type="button" className="btn link" onClick={() => void copy()}>
        {copied ? "Copied" : "Copy"}
      </button>
      <span className="sr-only" aria-live="polite">{copied ? "Fingerprint copied" : ""}</span>
    </div>
  );
}

export function Stepper({ state }: { state: LifecycleState }) {
  const currentIndex = lifecycleSteps.findIndex((step) => step.key === state.current);
  return (
    <ol className="stepper" aria-label="Change lifecycle">
      {lifecycleSteps.map((step, index) => {
        const skipped = state.skipped.includes(step.key);
        const done = index < currentIndex && !skipped;
        const current = index === currentIndex;
        const complete = current && step.key === "matches" && !state.problem;
        const cls = [skipped ? "skipped" : "", done || complete ? "done" : "", current && !complete ? "current" : "", current && state.problem ? "problem" : ""].join(" ");
        return (
          <li key={step.key} className={cls} aria-current={current ? "step" : undefined}>
            <span className="n" aria-hidden>{done || complete ? <Icon name="check" size={11} /> : skipped ? "–" : index + 1}</span>
            <span>
              {step.label}
              {skipped && <span className="sr-only"> (not needed)</span>}
              {current && state.problem && <span className="sr-only"> (needs attention)</span>}
            </span>
          </li>
        );
      })}
    </ol>
  );
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <strong>{title}</strong>
      {children}
    </div>
  );
}

export function Banner({ tone, children, onDismiss }: { tone: "critical" | "info" | "attention" | "positive"; children: ReactNode; onDismiss?: () => void }) {
  return (
    <div className={`banner ${tone}`} role={tone === "critical" ? "alert" : "status"}>
      <div>{children}</div>
      {onDismiss && (
        <button type="button" className="btn link" onClick={onDismiss} aria-label="Dismiss">
          <Icon name="cross" size={14} />
        </button>
      )}
    </div>
  );
}

/** Modal dialog: labelled, focus moves in and is trapped, Escape closes, focus returns. */
export function Dialog({
  title,
  children,
  footer,
  onClose,
}: {
  title: string;
  children: ReactNode;
  footer: ReactNode;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const node = ref.current;
    const focusables = () =>
      [...(node?.querySelectorAll<HTMLElement>("button, input, textarea, [href], [tabindex]:not([tabindex='-1'])") ?? [])].filter((el) => !el.hasAttribute("disabled"));
    (node?.querySelector<HTMLElement>("[data-autofocus]") ?? focusables()[0])?.focus();
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        closeRef.current();
      } else if (event.key === "Tab") {
        const items = focusables();
        if (!items.length) return;
        const first = items[0]!;
        const last = items[items.length - 1]!;
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }
    }
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      previous?.focus?.();
    };
  }, []);

  return (
    <div className="scrim" onMouseDown={onClose}>
      <div ref={ref} className="dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} onMouseDown={(event) => event.stopPropagation()}>
        <div className="dialog-head"><h2 id={titleId}>{title}</h2></div>
        <div className="dialog-body">{children}</div>
        <div className="dialog-foot">{footer}</div>
      </div>
    </div>
  );
}
