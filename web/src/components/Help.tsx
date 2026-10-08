import { useEffect, useRef, useState, type ReactNode } from "react";

/**
 * 見出しの横に置く「?」。押すと使い方の説明を開く（docs/plan.md「2.18」: 説明文は常に出さない）。
 * 外側をクリックするか Esc で閉じる。
 */
export function Help({
  children,
  label = "説明を見る",
}: {
  children: ReactNode;
  label?: string;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) =>
      ref.current && !ref.current.contains(e.target as Node) && setOpen(false);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);
  return (
    <span ref={ref} className="relative ml-1 inline-block align-middle">
      <button
        type="button"
        aria-label={label}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="inline-flex size-4 items-center justify-center rounded-full border border-slate-300 text-[10px] leading-none font-semibold text-slate-500 hover:bg-slate-100 hover:text-slate-700"
      >
        ?
      </button>
      {open && (
        <span
          role="note"
          className="absolute top-6 left-0 z-30 block w-80 rounded-md border border-slate-200 bg-white p-3 text-xs leading-relaxed font-normal whitespace-normal text-slate-600 shadow-lg"
        >
          {children}
        </span>
      )}
    </span>
  );
}
