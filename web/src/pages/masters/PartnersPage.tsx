import { useMemo, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { activityStatusLabels, partnerDirectionLabels, type List, type Partner, type PartnerDirection, type PartnerLine } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { CsvActions } from '../../components/CsvTransfer'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { Link, navigate, useLocation } from '../../lib/router'
import { useApi } from '../../lib/useApi'

/**
 * 取引先の管理画面（docs/plan.md「2.29」）。施策の相手（顧客・発注先）で、内訳ごとに付ける。
 * 登録・変更は FP&A だけ。だれでも、取引先が付いた施策と内訳を見られる（?id= で開く）。
 */
export function PartnersPage() {
  const user = useCurrentUser()
  const canWrite = user.role === 'fpa_admin'
  const { search } = useLocation()
  const { data, error, loading, reload } = useApi<List<Partner>>('/partners')
  const partners = useMemo(() => data?.items ?? [], [data])
  const companies = useMemo(() => [...new Set(partners.map((p) => p.company_name))].sort((a, b) => a.localeCompare(b, 'ja')), [partners])
  const [q, setQ] = useState('')
  const [direction, setDirection] = useState<'' | PartnerDirection>('')
  const [showStopped, setShowStopped] = useState(false)
  const [editing, setEditing] = useState<Partner | 'new' | null>(null)
  const [deleting, setDeleting] = useState<Partner | null>(null)
  const openId = Number(search.get('id')) || null
  const opened = partners.find((p) => p.id === openId)

  const keyword = q.trim().toLowerCase()
  const visible = partners.filter(
    (p) =>
      (showStopped || p.is_active || p.id === openId) &&
      (!direction || p.direction === direction) &&
      (!keyword || [p.code, p.name, p.company_name, p.accounting_code ?? ''].some((s) => s.toLowerCase().includes(keyword))),
  )
  const stoppedCount = partners.filter((p) => !p.is_active).length

  return (
    <>
      <PageHeader
        title="取引先"
        description="施策の相手（顧客・発注先）です。内訳ごとに付けられます。部署名が変わったら名前を直します（紐づきはそのまま）。統合・分割では新しい取引先を作り、古いほうを取引停止にします。"
        actions={
          <>
            <CsvActions
              resource="partners"
              label="取引先"
              canImport={canWrite}
              columns="code,name,company_name,accounting_code,direction,is_active,note"
              notes={
                <>
                  <p>取引先コードで突き合わせます。direction は sales（受注）/ purchase（発注）。受注・発注と書いても取り込めます。</p>
                  <p>is_active は true（取引中）/ false（取引停止）。accounting_code・is_active・note の列を省略すると、今の値を変えません。</p>
                </>
              }
              onImported={reload}
            />
            {canWrite && (
              <Button variant="primary" onClick={() => setEditing('new')}>
                ＋ 取引先を追加
              </Button>
            )}
          </>
        }
      />
      <Card>
        <div className="mb-3 flex flex-wrap items-end gap-3 text-sm">
          <label className="space-y-1">
            <span className="block text-xs font-medium text-slate-500">検索</span>
            <Input aria-label="取引先の検索" value={q} onChange={(e) => setQ(e.target.value)} placeholder="名前・会社名・コード" className="w-60 py-1" />
          </label>
          <label className="space-y-1">
            <span className="block text-xs font-medium text-slate-500">区分</span>
            <Select aria-label="区分" value={direction} onChange={(e) => setDirection(e.target.value as '' | PartnerDirection)} className="w-28 py-1">
              <option value="">すべて</option>
              <option value="sales">受注</option>
              <option value="purchase">発注</option>
            </Select>
          </label>
          <label className="flex items-center gap-1.5 pb-1.5 text-slate-600">
            <input type="checkbox" className="size-4 rounded border-slate-300" checked={showStopped} onChange={(e) => setShowStopped(e.target.checked)} />
            取引停止も表示（{stoppedCount}件）
          </label>
        </div>
        {loading && !data ? (
          <Loading />
        ) : error ? (
          <ErrorMessage error={error} />
        ) : visible.length === 0 ? (
          <Empty>{partners.length === 0 ? '取引先が登録されていません' : '条件に合う取引先はありません'}</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-28">コード</th>
                <th>取引先名</th>
                <th>会社名</th>
                <th className="w-32">会計システムコード</th>
                <th className="w-20">区分</th>
                <th className="w-20 text-right">内訳</th>
                {canWrite && <th className="w-32" />}
              </tr>
            </thead>
            <tbody>
              {visible.map((p) => (
                <tr key={p.id} className={p.id === openId ? 'bg-indigo-50/60' : undefined}>
                  <td className="font-mono text-xs">{p.code}</td>
                  <td className="font-medium">
                    <button type="button" className="text-left text-indigo-700 hover:underline" onClick={() => navigate(`/masters/partners?id=${p.id}`)}>
                      {p.name}
                    </button>
                    {!p.is_active && (
                      <span className="ml-2">
                        <Badge tone="slate">取引停止</Badge>
                      </span>
                    )}
                    {p.note && <span className="block text-xs font-normal text-slate-500">{p.note}</span>}
                  </td>
                  <td className="text-slate-700">{p.company_name}</td>
                  <td className="font-mono text-xs text-slate-600">{p.accounting_code ?? ''}</td>
                  <td>
                    <Badge tone={p.direction === 'sales' ? 'green' : 'amber'}>{partnerDirectionLabels[p.direction]}</Badge>
                  </td>
                  <td className="text-right tabular-nums">{p.line_count}</td>
                  {canWrite && (
                    <td className="text-right">
                      <Button size="sm" variant="ghost" onClick={() => setEditing(p)}>
                        編集
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setDeleting(p)}>
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

      {opened && <PartnerLinesCard key={opened.id} partner={opened} onClose={() => navigate('/masters/partners')} />}

      {editing && (
        <PartnerDialog
          initial={editing === 'new' ? null : editing}
          companies={companies}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="取引先の削除"
        message={<>「{deleting?.code} {deleting?.name}」を削除します。内訳に付いている取引先は削除できません（取引停止にしてください）。</>}
        reason="optional"
        onClose={() => setDeleting(null)}
        onConfirm={async (reason) => {
          await api.del(`/partners/${deleting!.id}`, { reason })
          await reload()
        }}
      />
    </>
  )
}

/** 取引先が付いた施策と内訳 */
function PartnerLinesCard({ partner, onClose }: { partner: Partner; onClose: () => void }) {
  const { data, error } = useApi<List<PartnerLine>>(`/partners/${partner.id}/lines`)
  return (
    <Card
      title={`${partner.name}（${partner.company_name}・${partnerDirectionLabels[partner.direction]}）が付いた内訳`}
      className="mt-4"
      actions={
        <Button size="sm" variant="ghost" onClick={onClose}>
          閉じる
        </Button>
      }
    >
      {error ? (
        <ErrorMessage error={error} />
      ) : !data ? (
        <Loading />
      ) : data.items.length === 0 ? (
        <Empty>この取引先が付いた内訳はありません。施策の「設定」の内訳の一覧で付けられます。</Empty>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>施策</th>
              <th className="w-24">ステータス</th>
              <th>科目</th>
              <th>内訳</th>
            </tr>
          </thead>
          <tbody>
            {data.items.map((l) => (
              <tr key={l.id}>
                <td>
                  <Link to={`/activities/${l.activity_id}?tab=settings`} className="text-indigo-700 hover:underline">
                    <span className="mr-1 font-mono text-xs text-slate-500">{l.activity_code}</span>
                    {l.activity_name}
                  </Link>
                </td>
                <td className="text-xs text-slate-600">{activityStatusLabels[l.status]}</td>
                <td className="text-slate-600">{l.subject_name}</td>
                <td className="font-medium">{l.name}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Card>
  )
}

function PartnerDialog({ initial, companies, onClose, onSaved }: { initial: Partner | null; companies: string[]; onClose: () => void; onSaved: () => void }) {
  const [code, setCode] = useState(initial?.code ?? '')
  const [name, setName] = useState(initial?.name ?? '')
  const [company, setCompany] = useState(initial?.company_name ?? '')
  const [accounting, setAccounting] = useState(initial?.accounting_code ?? '')
  const [direction, setDirection] = useState<PartnerDirection>(initial?.direction ?? 'sales')
  const [active, setActive] = useState(initial?.is_active ?? true)
  const [note, setNote] = useState(initial?.note ?? '')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const linked = (initial?.line_count ?? 0) > 0

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body = { code, name, company_name: company, accounting_code: accounting, direction, is_active: active, note, reason }
    try {
      if (initial) await api.put(`/partners/${initial.id}`, body)
      else await api.post('/partners', body)
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
      title={initial ? '取引先の編集' : '取引先の追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="partner-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="partner-form" onSubmit={submit} className="space-y-4">
        <Field label="取引先名" required error={fieldError(error, 'name')} hint="部署・窓口まで含めた呼び名（例: A社 情報システム部）。部署がなければ会社名と同じで構いません">
          {(p) => <Input {...p} autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />}
        </Field>
        <Field label="会社名" required error={fieldError(error, 'company_name')} hint="登録済みの会社名から選ぶと、表記がそろいます">
          {(p) => (
            <>
              <Input {...p} value={company} onChange={(e) => setCompany(e.target.value)} list="partner-companies" maxLength={100} />
              <datalist id="partner-companies">
                {companies.map((c) => (
                  <option key={c} value={c} />
                ))}
              </datalist>
            </>
          )}
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="区分" required error={fieldError(error, 'direction')} hint={linked ? '内訳に付いているため変えられません' : '受注も発注もある部署は、2件作ります'}>
            {(p) => (
              <Select {...p} value={direction} disabled={linked} onChange={(e) => setDirection(e.target.value as PartnerDirection)}>
                <option value="sales">受注（顧客）</option>
                <option value="purchase">発注（発注先・外注先）</option>
              </Select>
            )}
          </Field>
          <Field label="会計システムコード" error={fieldError(error, 'accounting_code')} hint="得意先・仕入先のコード（任意）">
            {(p) => <Input {...p} value={accounting} onChange={(e) => setAccounting(e.target.value)} className="font-mono" maxLength={50} />}
          </Field>
        </div>
        <Field label="取引先コード" error={fieldError(error, 'code')} hint={initial ? 'アプリ内で取引先を指す番号' : '空欄なら自動で採番します（TP-0001 形式）'}>
          {(p) => <Input {...p} value={code} onChange={(e) => setCode(e.target.value)} className="w-48 font-mono" placeholder={initial ? undefined : '自動採番'} />}
        </Field>
        <label className="flex items-start gap-2 text-sm text-slate-700">
          <input type="checkbox" className="mt-0.5 size-4 rounded border-slate-300" checked={!active} onChange={(e) => setActive(!e.target.checked)} />
          <span>
            取引停止
            <span className="block text-xs text-slate-500">内訳の選択肢に出さなくなります。付いている内訳はそのままです。</span>
          </span>
        </label>
        <Field label="メモ" error={fieldError(error, 'note')} hint="例: 2027年4月に B社 DX推進部へ統合">
          {(p) => <Textarea {...p} value={note} onChange={(e) => setNote(e.target.value)} className="min-h-12" maxLength={500} />}
        </Field>
        <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['code', 'name', 'company_name', 'accounting_code', 'direction', 'note', 'reason']} />
      </form>
    </Dialog>
  )
}
