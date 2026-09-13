import { ReactNode, useEffect, useId, useRef, useState } from "react";
import { lifecycleSteps, type LifecycleState, type Tone } from "./model.js";

// ---------------------------------------------------------------------------
// Icons: 16px stroke icons, inline so the console makes no asset requests.

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
  search: "M7 12.5A5.5 5.5 0 1 0 7 1.5a5.5 5.5 0 0 0 0 11zM11 11l3.5 3.5",
  plus: "M8 3v10M3 8h10",
  back: "M13 8H3M6.5 4.5L3 8l3.5 3.5",
  calendar: "M2.5 4h11v9.5h-11zM2.5 7h11M5.5 2.5v3M10.5 2.5v3",
  doc: "M4 1.5h5.5L12.5 4.5v10h-8.5zM9 1.5v3.5h3.5M6 8.5h4.5M6 11h4.5",
  sparkle: "M8 2l1.4 4.6L14 8l-4.6 1.4L8 14l-1.4-4.6L2 8l4.6-1.4z",
  flask: "M6 2h4M6.5 2v4L3 13.5h10L9.5 6V2",
  rollout: "M2.5 8h8M8 4.5L11.5 8 8 11.5M13.5 3v10",
  filter: "M8 3v10M3 8h10",
  chevronDown: "M3.5 6L8 10.5 12.5 6",
  send: "M14 2L7 9M14 2l-4.5 12-2.5-5-5-2.5z",
  lightbulb: "M6 13.5h4M6.5 11.5h3M8 1.5a4.5 4.5 0 0 0-2.5 8.2V11h5V9.7A4.5 4.5 0 0 0 8 1.5z",
  pin: "M8 14s4.5-4.2 4.5-7.5a4.5 4.5 0 1 0-9 0C3.5 9.8 8 14 8 14z",
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

export function CopyField({ value, display, label }: { value: string; display?: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <span className="copy-field">
      <code title={value}>{display ?? value}</code>
      <button
        type="button"
        className="icon-btn"
        aria-label={copied ? `${label} copied` : `Copy ${label}`}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          } catch {
            /* clipboard unavailable; value remains visible in the title */
          }
        }}
      >
        <Icon name={copied ? "check" : "copy"} size={13} />
      </button>
    </span>
  );
}

export function Tabs<T extends string>({ tabs, value, onChange, label, big }: { tabs: Array<{ key: T; label: string; count?: number | string }>; value: T; onChange: (key: T) => void; label: string; big?: boolean }) {
  return (
    <div className={`tabs ${big ? "big" : ""}`} role="tablist" aria-label={label}>
      {tabs.map((tab) => (
        <button key={tab.key} type="button" role="tab" aria-selected={tab.key === value} onClick={() => onChange(tab.key)}>
          {tab.label}
          {tab.count !== undefined && <span className="count">{tab.count}</span>}
        </button>
      ))}
    </div>
  );
}

export function Breadcrumbs({ items }: { items: Array<{ label: string; onClick?: () => void }> }) {
  return (
    <nav className="crumbs" aria-label="Breadcrumb">
      {items.map((item, index) => {
        const last = index === items.length - 1;
        return (
          <span key={`${item.label}-${index}`} style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
            {last || !item.onClick ? (
              <span aria-current={last ? "page" : undefined}>{item.label}</span>
            ) : (
              <button type="button" onClick={item.onClick}>{item.label}</button>
            )}
            {!last && <Icon name="chevron" size={14} />}
          </span>
        );
      })}
    </nav>
  );
}

export function RadioCards<T extends string>({ options, value, onChange, label }: { options: Array<{ key: T; title: ReactNode; description: ReactNode }>; value: T | null; onChange: (key: T) => void; label: string }) {
  return (
    <div className="radio-cards" role="radiogroup" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.key}
          type="button"
          role="radio"
          aria-checked={option.key === value}
          className="radio-card"
          onClick={() => onChange(option.key)}
        >
          <span className="dotbox" aria-hidden />
          <span>
            <strong>{option.title}</strong>
            <span className="desc">{option.description}</span>
          </span>
        </button>
      ))}
    </div>
  );
}

export function AccordionItem({
  icon,
  title,
  subtitle,
  aside,
  open,
  onToggle,
  children,
}: {
  icon: IconName;
  title: ReactNode;
  subtitle?: ReactNode;
  aside?: ReactNode;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  const bodyId = useId();
  return (
    <div className={`acc ${open ? "open" : ""}`}>
      <button type="button" className="acc-head" aria-expanded={open} aria-controls={bodyId} onClick={onToggle}>
        <Icon name={icon} />
        <span>
          <span className="t">{title}</span>
          {subtitle && <span className="s">{subtitle}</span>}
        </span>
        <span>{aside}</span>
        <span className="chev"><Icon name="chevronDown" size={18} /></span>
      </button>
      {open && <div className="acc-body" id={bodyId}>{children}</div>}
    </div>
  );
}

export function StatusCards<T extends string>({ cards, value, onChange, label }: { cards: Array<{ key: T; label: string; count: number | string; icon?: IconName }>; value: T; onChange: (key: T) => void; label: string }) {
  return (
    <div className="status-cards" role="group" aria-label={label}>
      {cards.map((card) => (
        <button key={card.key} type="button" className="status-card" aria-pressed={card.key === value} onClick={() => onChange(card.key)}>
          <span>{card.icon && <Icon name={card.icon} size={13} />}{card.label}</span>
          <strong>{card.count}</strong>
        </button>
      ))}
    </div>
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
      <button type="button" className="link" onClick={() => setExpanded((value) => !value)} aria-expanded={expanded}>
        {expanded ? "Less" : "Full"}
      </button>
      <button type="button" className="link" onClick={() => void copy()}>
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
    <div className={`callout ${tone}`} role={tone === "critical" ? "alert" : "status"}>
      <div>{children}</div>
      {onDismiss && (
        <button type="button" className="icon-btn" onClick={onDismiss} aria-label="Dismiss">
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
        <div className="dialog-head">
          <h2 id={titleId}>{title}</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={() => closeRef.current()}><Icon name="cross" size={14} /></button>
        </div>
        <div className="dialog-body">{children}</div>
        <div className="dialog-foot">{footer}</div>
      </div>
    </div>
  );
}
