import { useEffect, useRef } from "react";
import { Link } from "react-router-dom";
import type { PackSummary } from "../../api/types";
import { Icon } from "../../components/Icon";

export function PackList({ packs }: { packs: PackSummary[] }) {
  if (packs.length === 0) {
    return <p className="notice warning">No domain pack can start a case right now. Packs need an intake and a live Flowcase workflow; see the Domain Pack Library for reasons.</p>;
  }
  return (
    <ul className="pack-pick">
      {packs.map((p) => (
        <li key={`${p.pack_key}@${p.version}`}>
          <Link to={`/cases/new?pack=${encodeURIComponent(p.pack_key)}`} className="pack-pick-item">
            <span className="pack-icon"><Icon name={p.brand?.icon ?? "blocks"} /></span>
            <span><strong>{p.title}</strong><small>{p.description ?? `${p.pack_key} v${p.version}`}</small></span>
            <Icon name="arrow-right" size="sm" />
          </Link>
        </li>
      ))}
    </ul>
  );
}

export function PackPickerDialog({ packs, onClose }: { packs: PackSummary[]; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    ref.current?.focus();
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", onKey);
    return () => { document.removeEventListener("keydown", onKey); prev?.focus?.(); };
  }, [onClose]);
  return (
    <div className="modal-back show" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="pack-picker-title" ref={ref} tabIndex={-1}>
        <h2 id="pack-picker-title">Start a new case</h2>
        <p>Pick the domain pack. A short chat with the intake assistant prepares the case brief; nothing is created until you confirm.</p>
        <PackList packs={packs} />
        <div className="modal-foot"><button className="btn" type="button" onClick={onClose}>Cancel</button></div>
      </div>
    </div>
  );
}
