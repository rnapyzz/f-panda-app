import { useMemo, useState } from 'react'
import type { Account, Business, Department } from '../../api/dimensions'
import type { AxisDimension, AxisLabelInput, CreateBindingRequest, FixedDimensions } from '../../api/inputsheet'
import type { Period } from '../../api/periods'
import type { SelectionSnapshot } from '../../lib/univer/UniverSheet'
import { buttonPrimary, buttonSecondary, errorText, fieldset, input, label as labelClass, legend, select } from '../../lib/ui'

const AXIS_DIMENSION_LABELS: Record<AxisDimension, string> = {
  account: '勘定科目',
  period: '期間',
  business: '事業',
  department: '部門',
}

const ALL_AXIS_DIMENSIONS: AxisDimension[] = ['account', 'period', 'business', 'department']

interface Option {
  id: number
  text: string
}

function optionsFor(dim: AxisDimension, data: { businesses: Business[]; departments: Department[]; accounts: Account[]; periods: Period[] }): Option[] {
  switch (dim) {
    case 'business':
      return data.businesses.map((b) => ({ id: b.id, text: b.name }))
    case 'department':
      return data.departments.map((d) => ({ id: d.id, text: d.name }))
    case 'account':
      return data.accounts.map((a) => ({ id: a.id, text: a.name }))
    case 'period':
      return data.periods.map((p) => ({ id: p.id, text: p.label }))
  }
}

function fixedDimKey(dim: AxisDimension): keyof FixedDimensions {
  return `${dim}_id` as keyof FixedDimensions
}

interface Props {
  selection: SelectionSnapshot
  businesses: Business[]
  departments: Department[]
  accounts: Account[]
  periods: Period[]
  onCancel: () => void
  onSave: (input: CreateBindingRequest) => Promise<void>
}

export function BindingWizard({ selection, businesses, departments, accounts, periods, onCancel, onSave }: Props) {
  const dimData = { businesses, departments, accounts, periods }

  const [name, setName] = useState('')
  const [rowAxis, setRowAxis] = useState<AxisDimension>('account')
  const [colAxis, setColAxis] = useState<AxisDimension>('period')
  const [fixedValues, setFixedValues] = useState<Partial<Record<AxisDimension, number>>>({})
  const [rowMappings, setRowMappings] = useState<Record<number, number | ''>>({})
  const [colMappings, setColMappings] = useState<Record<number, number | ''>>({})
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const rowLabels = useMemo(() => selection.values.slice(1).map((r) => r[0] ?? ''), [selection])
  const colLabels = useMemo(() => (selection.values[0] ?? []).slice(1), [selection])
  const fixedDimensions = ALL_AXIS_DIMENSIONS.filter((d) => d !== rowAxis && d !== colAxis)

  const tooSmall = rowLabels.length === 0 || colLabels.length === 0

  async function handleSubmit() {
    setError(null)
    if (tooSmall) {
      setError('範囲が小さすぎます（見出し行・見出し列に加えてデータが必要です）')
      return
    }
    if (rowAxis === colAxis) {
      setError('行の軸と列の軸は異なる項目を選んでください')
      return
    }
    if (!name.trim()) {
      setError('名称を入力してください')
      return
    }
    for (const dim of fixedDimensions) {
      if (!fixedValues[dim]) {
        setError(`${AXIS_DIMENSION_LABELS[dim]}を選択してください`)
        return
      }
    }
    const axisLabels: AxisLabelInput[] = []
    for (let i = 0; i < rowLabels.length; i++) {
      const id = rowMappings[i]
      if (!id) {
        setError(`行ラベル「${rowLabels[i]}」の対応先を選択してください`)
        return
      }
      axisLabels.push({ axis: 'row', axis_index: i, raw_label_text: rowLabels[i], resolved_dimension_type: rowAxis, resolved_dimension_id: id })
    }
    for (let i = 0; i < colLabels.length; i++) {
      const id = colMappings[i]
      if (!id) {
        setError(`列ラベル「${colLabels[i]}」の対応先を選択してください`)
        return
      }
      axisLabels.push({ axis: 'col', axis_index: i, raw_label_text: colLabels[i], resolved_dimension_type: colAxis, resolved_dimension_id: id })
    }

    const fixed: FixedDimensions = {}
    for (const dim of fixedDimensions) {
      fixed[fixedDimKey(dim)] = fixedValues[dim]
    }

    setSaving(true)
    try {
      await onSave({
        name,
        range_sheet_name: selection.sheetName,
        start_row: selection.startRow,
        end_row: selection.endRow,
        start_col: selection.startColumn,
        end_col: selection.endColumn,
        header_rows: 1,
        header_cols: 1,
        row_axis_dimension: rowAxis,
        col_axis_dimension: colAxis,
        fixed_dimensions: fixed,
        axis_labels: axisLabels,
      })
    } catch {
      setError('バインディングの保存に失敗しました')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="rounded border border-gray-300 bg-white p-4 shadow-lg dark:border-gray-600 dark:bg-gray-900">
      <h3 className="mb-2 text-base font-semibold text-gray-900 dark:text-gray-100">バインディングを作成</h3>
      <p className="mb-3 text-sm text-gray-500 dark:text-gray-400">
        選択範囲: {selection.sheetName}!{cellRef(selection.startRow, selection.startColumn)}:
        {cellRef(selection.endRow, selection.endColumn)}
      </p>

      {tooSmall ? (
        <p className={errorText}>範囲が小さすぎます。見出し行・見出し列に加えて、最低1行1列のデータが必要です。</p>
      ) : (
        <>
          <fieldset className={fieldset}>
            <legend className={legend}>基本設定</legend>
            <label className={labelClass}>
              名称
              <input className={input} value={name} onChange={(e) => setName(e.target.value)} placeholder="例: 4-5月 人件費・地代家賃" />
            </label>
            <div className="flex flex-wrap gap-4">
              <label className={labelClass}>
                行の軸（1列目の見出し）
                <select className={select} value={rowAxis} onChange={(e) => setRowAxis(e.target.value as AxisDimension)}>
                  {ALL_AXIS_DIMENSIONS.map((d) => (
                    <option key={d} value={d}>
                      {AXIS_DIMENSION_LABELS[d]}
                    </option>
                  ))}
                </select>
              </label>
              <label className={labelClass}>
                列の軸（1行目の見出し）
                <select className={select} value={colAxis} onChange={(e) => setColAxis(e.target.value as AxisDimension)}>
                  {ALL_AXIS_DIMENSIONS.map((d) => (
                    <option key={d} value={d}>
                      {AXIS_DIMENSION_LABELS[d]}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            {fixedDimensions.length > 0 && (
              <div className="flex flex-wrap gap-4">
                {fixedDimensions.map((dim) => (
                  <label key={dim} className={labelClass}>
                    {AXIS_DIMENSION_LABELS[dim]}（この範囲全体で固定）
                    <select
                      className={select}
                      value={fixedValues[dim] ?? ''}
                      onChange={(e) => setFixedValues((v) => ({ ...v, [dim]: e.target.value ? Number(e.target.value) : undefined }))}
                    >
                      <option value="">選択してください</option>
                      {optionsFor(dim, dimData).map((o) => (
                        <option key={o.id} value={o.id}>
                          {o.text}
                        </option>
                      ))}
                    </select>
                  </label>
                ))}
              </div>
            )}
          </fieldset>

          <fieldset className={fieldset}>
            <legend className={legend}>行ラベルの対応付け（{AXIS_DIMENSION_LABELS[rowAxis]}）</legend>
            {rowLabels.map((text, i) => (
              <div key={i} className="flex items-center gap-2">
                <span className="w-32 truncate text-sm">{text || '(空欄)'}</span>
                <select
                  className={select}
                  value={rowMappings[i] ?? ''}
                  onChange={(e) => setRowMappings((m) => ({ ...m, [i]: e.target.value ? Number(e.target.value) : '' }))}
                >
                  <option value="">選択してください</option>
                  {optionsFor(rowAxis, dimData).map((o) => (
                    <option key={o.id} value={o.id}>
                      {o.text}
                    </option>
                  ))}
                </select>
              </div>
            ))}
          </fieldset>

          <fieldset className={fieldset}>
            <legend className={legend}>列ラベルの対応付け（{AXIS_DIMENSION_LABELS[colAxis]}）</legend>
            {colLabels.map((text, i) => (
              <div key={i} className="flex items-center gap-2">
                <span className="w-32 truncate text-sm">{text || '(空欄)'}</span>
                <select
                  className={select}
                  value={colMappings[i] ?? ''}
                  onChange={(e) => setColMappings((m) => ({ ...m, [i]: e.target.value ? Number(e.target.value) : '' }))}
                >
                  <option value="">選択してください</option>
                  {optionsFor(colAxis, dimData).map((o) => (
                    <option key={o.id} value={o.id}>
                      {o.text}
                    </option>
                  ))}
                </select>
              </div>
            ))}
          </fieldset>
        </>
      )}

      {error && (
        <p role="alert" className={`${errorText} mb-2`}>
          {error}
        </p>
      )}

      <div className="flex gap-2">
        <button type="button" className={buttonPrimary} onClick={() => void handleSubmit()} disabled={saving || tooSmall}>
          {saving ? '保存中...' : 'バインディングを保存'}
        </button>
        <button type="button" className={buttonSecondary} onClick={onCancel}>
          キャンセル
        </button>
      </div>
    </div>
  )
}

function cellRef(row: number, col: number): string {
  let colLabel = ''
  let n = col
  do {
    colLabel = String.fromCharCode(65 + (n % 26)) + colLabel
    n = Math.floor(n / 26) - 1
  } while (n >= 0)
  return `${colLabel}${row + 1}`
}
