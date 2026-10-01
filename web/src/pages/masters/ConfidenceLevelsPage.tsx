import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import type { ConfidenceLevel, List } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { ratePercent } from '../../lib/confidence'
import { useApi } from '../../lib/useApi'

/** 確度の段階の管理画面。段階ごとに標準の確率と、事実で判断できる判定基準を持つ */
export function ConfidenceLevelsPage() {
  const user = useCurrentUser()
  const canWrite = user.role === 'fpa_admin'
  const { data, error, loading, reload } = useApi<List<ConfidenceLevel>>('/confidence-levels')
  const levels = data?.items ?? []
  const [editing, setEditing] = useState<ConfidenceLevel | 'new' | null>(null)
  const [deleting, setDeleting] = useState<ConfidenceLevel | null>(null)

  return (
    <>
      <PageHeader
        title="確度の段階"
        description="施策・内訳の確度は、この段階から選びます。判定基準は、担当者が段階を選ぶときに表示されます。標準の確率は、加重見込・楽観・悲観の集計に使います。"
        actions={
          canWrite && (
            <Button variant="primary" onClick={() => setEditing('new')}>
              ＋ 段階を追加
            </Button>
          )
        }
      />
      <Card>
        {loading && !data ? (
          <Loading />
        ) : error ? (
          <ErrorMessage error={error} />
        ) : levels.length === 0 ? (
          <Empty>確度の段階が登録されていません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-20">コード</th>
                <th className="w-32">名前</th>
                <th className="w-24 text-right">標準の確率</th>
                <th>判定基準</th>
                <th className="w-20 text-right">表示順</th>
                {canWrite && <th className="w-32" />}
              </tr>
            </thead>
            <tbody>
              {levels.map((l) => (
                <tr key={l.id}>
                  <td className="font-mono text-sm font-semibold">{l.code}</td>
                  <td className="font-medium">{l.name}</td>
                  <td className="text-right tabular-nums">{ratePercent(l.rate)}</td>
                  <td className="text-slate-600">{l.criteria}</td>
                  <td className="text-right tabular-nums">{l.sort_order}</td>
                  {canWrite && (
                    <td className="text-right">
                      <Button size="sm" variant="ghost" onClick={() => setEditing(l)}>
                        編集
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setDeleting(l)}>
                        削除
                      </Button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {editing && (
        <LevelDialog
          initial={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="確度の段階の削除"
        message={<>「{deleting?.code} {deleting?.name}」を削除します。施策で使われている段階は削除できません。</>}
        reason="optional"
        onClose={() => setDeleting(null)}
        onConfirm={async (reason) => {
          await api.del(`/confidence-levels/${deleting!.id}`, { reason })
          await reload()
        }}
      />
    </>
  )
}

function LevelDialog({ initial, onClose, onSaved }: { initial: ConfidenceLevel | null; onClose: () => void; onSaved: () => void }) {
  const [code, setCode] = useState(initial?.code ?? '')
  const [name, setName] = useState(initial?.name ?? '')
  const [percent, setPercent] = useState(initial ? String(Math.round(Number(initial.rate) * 10000) / 100) : '')
  const [criteria, setCriteria] = useState(initial?.criteria ?? '')
  const [sortOrder, setSortOrder] = useState(String(initial?.sort_order ?? 0))
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    // 画面はパーセントで入力し、API には 0〜1 で送る（小数の誤差を避けるため文字列で計算する）
    const rate = percent.trim() === '' ? null : String(Math.round(Number(percent) * 100) / 10000)
    const body = { code, name, rate, criteria, sort_order: Number(sortOrder) || 0, reason }
    try {
      if (initial) await api.put(`/confidence-levels/${initial.id}`, body)
      else await api.post('/confidence-levels', body)
      onSaved()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      title={initial ? '確度の段階の編集' : '確度の段階の追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="level-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="level-form" onSubmit={submit} className="space-y-4">
        <div className="grid grid-cols-3 gap-3">
          <Field label="コード" required error={fieldError(error, 'code')} hint={initial ? '作成後は変更できません' : '英大文字・数字（例: A）'}>
            {(p) => <Input {...p} autoFocus={!initial} disabled={initial !== null} value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} className="font-mono" />}
          </Field>
          <Field label="名前" required error={fieldError(error, 'name')}>
            {(p) => <Input {...p} autoFocus={initial !== null} value={name} onChange={(e) => setName(e.target.value)} placeholder="例: 高" />}
          </Field>
          <Field label="標準の確率（%）" required error={fieldError(error, 'rate')}>
            {(p) => <Input {...p} type="number" min={0} max={100} step="0.01" value={percent} onChange={(e) => setPercent(e.target.value)} />}
          </Field>
        </div>
        <Field label="判定基準" error={fieldError(error, 'criteria')} hint="事実で判断できる文にします（例: 内示・口頭合意がある）">
          {(p) => <Textarea {...p} value={criteria} onChange={(e) => setCriteria(e.target.value)} className="min-h-16" />}
        </Field>
        <Field label="表示順" error={fieldError(error, 'sort_order')}>
          {(p) => <Input {...p} type="number" value={sortOrder} onChange={(e) => setSortOrder(e.target.value)} className="w-32" />}
        </Field>
        <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['code', 'name', 'rate', 'criteria', 'sort_order', 'reason']} />
      </form>
    </Dialog>
  )
}
