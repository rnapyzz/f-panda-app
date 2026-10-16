import { useState, type ReactNode } from 'react'
import { api } from '../../api/client'
import type { Scenario, ScenarioNarrative as Narrative } from '../../api/types'
import { Button, Field, FormError, Input, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { formatDateTime } from '../../lib/format'
import { useApi } from '../../lib/useApi'

/** 説明の欄（docs/plan.md「2.31」）。順番は画面に出す順 */
const narrativeFields = [
  { key: 'outline', label: '計画の骨子', hint: 'この計画で何を目指し、どういう業績を見込むか' },
  { key: 'growth', label: '成長の柱', hint: '売上・利益を何で伸ばすか（事業・重点施策）' },
  { key: 'premises', label: '主な前提', hint: '市況・価格・採用・大型案件など、計画が成り立つための前提' },
  { key: 'risks', label: '主なリスクと機会', hint: '前提が崩れたときに何が起き、どれくらい振れるか。上振れの余地' },
] as const

type FieldKey = (typeof narrativeFields)[number]['key']

/**
 * シナリオの説明。書くのは FP&A で、ロック済みのシナリオは直せない。
 * aside は編集中に入力欄の横に並べる「数字から見えること」。
 */
export function ScenarioNarrative({ scenario, aside }: { scenario: Scenario; aside: ReactNode }) {
  const me = useCurrentUser()
  const narrative = useApi<Narrative>(`/scenarios/${scenario.id}/narrative`)
  const [editing, setEditing] = useState(false)
  const canEdit = me.role === 'fpa_admin' && !scenario.is_locked
  const n = narrative.data
  if (!n) return null
  const written = narrativeFields.filter((f) => n[f.key] !== '')

  if (editing) {
    return (
      <NarrativeForm
        scenario={scenario}
        narrative={n}
        aside={aside}
        onCancel={() => setEditing(false)}
        onSaved={(saved) => {
          narrative.setData(saved)
          setEditing(false)
        }}
      />
    )
  }
  return (
    <section aria-label="このシナリオの説明" className="rounded-lg border border-slate-200 bg-white px-4 py-3">
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-sm font-semibold text-slate-800">このシナリオの説明</h2>
        <div className="flex items-center gap-3 text-xs text-slate-500">
          {n.updated_at && (
            <span>
              {n.updated_by_name}・{formatDateTime(n.updated_at)}
            </span>
          )}
          {canEdit && (
            <Button size="sm" variant={written.length === 0 ? 'primary' : 'secondary'} onClick={() => setEditing(true)}>
              {written.length === 0 ? '説明を書く' : '編集'}
            </Button>
          )}
        </div>
      </div>
      {written.length === 0 ? (
        <p className="text-sm text-slate-500">
          {canEdit ? '計画の骨子・成長の柱・主な前提・主なリスクと機会を書くと、ここに出ます。' : 'まだ説明がありません。'}
        </p>
      ) : (
        <dl className="grid gap-x-6 gap-y-3 md:grid-cols-2">
          {written.map((f) => (
            <div key={f.key} className="min-w-0">
              <dt className="text-xs font-semibold text-indigo-800">{f.label}</dt>
              <dd className="mt-0.5 text-sm leading-relaxed break-words whitespace-pre-wrap text-slate-800">{n[f.key]}</dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}

function NarrativeForm({ scenario, narrative, aside, onCancel, onSaved }: { scenario: Scenario; narrative: Narrative; aside: ReactNode; onCancel: () => void; onSaved: (n: Narrative) => void }) {
  const [values, setValues] = useState<Record<FieldKey, string>>({ outline: narrative.outline, growth: narrative.growth, premises: narrative.premises, risks: narrative.risks })
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [saving, setSaving] = useState(false)
  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      onSaved(await api.put<Narrative>(`/scenarios/${scenario.id}/narrative`, { ...values, reason }))
    } catch (e) {
      setError(e)
    } finally {
      setSaving(false)
    }
  }
  return (
    <section aria-label="このシナリオの説明" className="rounded-lg border border-indigo-200 bg-white px-4 py-3">
      <h2 className="mb-3 text-sm font-semibold text-slate-800">このシナリオの説明を書く</h2>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,26rem)]">
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          {narrativeFields.map((f) => (
            <Field key={f.key} label={f.label} hint={`${f.hint}（2,000 文字まで）`} error={fieldError(error, f.key)}>
              {(p) => <Textarea {...p} rows={4} value={values[f.key]} onChange={(e) => setValues({ ...values, [f.key]: e.target.value })} />}
            </Field>
          ))}
          <Field label="変更理由" hint={`空欄なら「${scenario.name}の説明を更新」と記録します`}>
            {(p) => <Input {...p} value={reason} onChange={(e) => setReason(e.target.value)} />}
          </Field>
          <FormError error={error} fields={[...narrativeFields.map((f) => f.key), 'reason']} />
          <div className="flex gap-2">
            <Button type="submit" variant="primary" disabled={saving}>
              保存
            </Button>
            <Button onClick={onCancel} disabled={saving}>
              取消
            </Button>
          </div>
        </form>
        <aside aria-label="書くときの手がかり" className="min-w-0">
          {aside}
        </aside>
      </div>
    </section>
  )
}
