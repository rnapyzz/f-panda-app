import { amountUnitLabels, type AmountUnit } from '../lib/format'
import { cx } from './ui'

/** 金額の表示単位の切り替え（百万円・千円・円）。画面を開くたびに百万円から始める */
export function AmountUnitSwitch({ value, onChange }: { value: AmountUnit; onChange: (u: AmountUnit) => void }) {
  return (
    <div className="inline-flex items-center gap-2 text-xs text-slate-500">
      <span>金額の単位</span>
      <div className="inline-flex rounded-md border border-slate-300 bg-white p-0.5" role="radiogroup" aria-label="金額の単位">
        {(Object.keys(amountUnitLabels) as AmountUnit[]).map((u) => (
          <button
            key={u}
            type="button"
            role="radio"
            aria-checked={value === u}
            onClick={() => onChange(u)}
            className={cx('rounded px-2 py-0.5', value === u ? 'bg-indigo-600 text-white' : 'text-slate-600 hover:bg-slate-100')}
          >
            {amountUnitLabels[u]}
          </button>
        ))}
      </div>
    </div>
  )
}
