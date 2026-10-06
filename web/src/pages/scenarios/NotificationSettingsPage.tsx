import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import {
  notificationKindDescriptions,
  notificationKindLabels,
  type List,
  type NotificationKind,
  type NotificationRun,
  type NotificationSettings,
} from '../../api/types'
import { Badge, Button, Card, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { formatDateTime } from '../../lib/format'
import { Link } from '../../lib/router'
import { useApi } from '../../lib/useApi'

const kinds = Object.keys(notificationKindLabels) as NotificationKind[]

/**
 * 通知の設定（FP&A のみ、docs/plan.md「2.13」）。通知の種類ごとの有効・無効、締切の前の日数、送信時刻、
 * Slack のテスト送信と送信の記録。
 */
export function NotificationSettingsPage() {
  const me = useCurrentUser()
  const settings = useApi<NotificationSettings>('/notification-settings')
  const runs = useApi<List<NotificationRun>>(me.role === 'fpa_admin' ? '/notification-runs' : null)
  if (me.role !== 'fpa_admin') {
    return <PageHeader title="通知の設定" description="この画面は FP&A のみが使えます。" />
  }
  return (
    <>
      <PageHeader
        title="通知の設定"
        description={
          <>
            作成中のシナリオの締切（
            <Link to="/admin/scenarios" className="text-indigo-700 hover:underline">
              シナリオ管理
            </Link>
            の「設定」で入力）に合わせて、アプリ内のお知らせと Slack で自動で通知します。宛先は未完了の施策の担当者（いなければユニットのマネージャー）です。
          </>
        }
      />
      {settings.error ? <ErrorMessage error={settings.error} /> : !settings.data ? <Loading /> : <SettingsForm initial={settings.data} onSaved={settings.setData} />}

      <Card title="送信の記録" className="mt-4">
        {runs.error ? (
          <ErrorMessage error={runs.error} />
        ) : !runs.data ? (
          <Loading />
        ) : runs.data.items.length === 0 ? (
          <Empty>まだ通知を送っていません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>日時</th>
                <th>種類</th>
                <th>シナリオ</th>
                <th className="text-right">宛先</th>
                <th>Slack</th>
              </tr>
            </thead>
            <tbody>
              {runs.data.items.map((r) => (
                <tr key={r.id}>
                  <td className="text-sm whitespace-nowrap">{formatDateTime(r.created_at)}</td>
                  <td>{notificationKindLabels[r.kind]}</td>
                  <td>{r.scenario_name}</td>
                  <td className="text-right tabular-nums">{r.recipients} 人</td>
                  <td>
                    {r.slack_status === 'sent' ? (
                      <Badge tone="green">送信済み</Badge>
                    ) : r.slack_status === 'failed' ? (
                      <span title={r.slack_error}>
                        <Badge tone="red">失敗（{r.slack_attempts} 回）</Badge>
                        <span className="ml-1 text-xs text-red-700">{r.slack_error}</span>
                      </span>
                    ) : (
                      <span className="text-xs text-slate-500">送らない（未設定）</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </>
  )
}

function SettingsForm({ initial, onSaved }: { initial: NotificationSettings; onSaved: (s: NotificationSettings) => void }) {
  const [enabled, setEnabled] = useState(initial.enabled_kinds)
  const [days, setDays] = useState(initial.reminder_days.join(','))
  const [sendTime, setSendTime] = useState(initial.send_time)
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)
  const [testResult, setTestResult] = useState<'ok' | unknown>(null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    setSaved(false)
    const reminderDays = days
      .split(/[,、\s]+/)
      .filter(Boolean)
      .map(Number)
    try {
      onSaved(await api.put<NotificationSettings>('/notification-settings', { enabled_kinds: enabled, reminder_days: reminderDays, send_time: sendTime, reason }))
      setSaved(true)
      setReason('')
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }
  const test = async () => {
    setTestResult(null)
    try {
      await api.post('/notification-settings/test', {})
      setTestResult('ok')
    } catch (err) {
      setTestResult(err)
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Card title="通知の種類と時刻" className="lg:col-span-2">
        <form onSubmit={submit} className="space-y-4">
          <fieldset>
            <legend className="mb-1 text-sm font-medium text-slate-700">送る通知</legend>
            <div className="space-y-2">
              {kinds.map((k) => (
                <label key={k} className="flex items-start gap-2 text-sm">
                  <input type="checkbox" className="mt-0.5 size-4 rounded border-slate-300" checked={enabled[k]} onChange={(e) => setEnabled({ ...enabled, [k]: e.target.checked })} />
                  <span>
                    {notificationKindLabels[k]}
                    <span className="block text-xs text-slate-500">{notificationKindDescriptions[k]}</span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="締切の何日前に知らせるか" error={fieldError(error, 'reminder_days')} hint="カンマ区切り（例: 3,1）。土日に当たる日は直前の金曜に送ります">
              {(p) => <Input {...p} value={days} onChange={(e) => setDays(e.target.value)} className="w-40" />}
            </Field>
            <Field label="送信時刻（日本時間）" error={fieldError(error, 'send_time')} hint="締切の前・超過の通知を送る時刻">
              {(p) => <Input {...p} type="time" value={sendTime} onChange={(e) => setSendTime(e.target.value)} className="w-32" />}
            </Field>
          </div>
          <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
            {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
          </Field>
          <FormError error={error} fields={['enabled_kinds', 'reminder_days', 'send_time', 'reason']} />
          <div className="flex items-center gap-3">
            <Button variant="primary" type="submit" disabled={busy}>
              {busy ? '保存中…' : '保存'}
            </Button>
            {saved && (
              <span className="text-sm text-emerald-700" role="status">
                保存しました
              </span>
            )}
          </div>
        </form>
      </Card>

      <Card title="Slack">
        {initial.slack_configured ? (
          <div className="space-y-3 text-sm">
            <p>
              <Badge tone="green">設定済み</Badge> 共有チャンネルに投稿し、担当者を @メンションします。
            </p>
            <p className="text-xs text-slate-500">
              メンションするには、
              <Link to="/masters/users" className="text-indigo-700 hover:underline">
                ユーザー
              </Link>
              に Slack のメンバー ID を登録してください。未登録の人は名前だけを書きます。
            </p>
            <Button onClick={test}>テスト送信</Button>
            {testResult === 'ok' ? (
              <p className="text-sm text-emerald-700" role="status">
                送信しました。チャンネルを確認してください。
              </p>
            ) : testResult ? (
              <ErrorMessage error={testResult} />
            ) : null}
          </div>
        ) : (
          <div className="space-y-2 text-sm text-slate-600">
            <p>
              <Badge>未設定</Badge> Slack には送りません（アプリ内のお知らせだけ）。
            </p>
            <p className="text-xs text-slate-500">
              Slack に送るには、共有チャンネルの Incoming Webhook を作り、その URL を API の環境変数 <code className="font-mono">SLACK_WEBHOOK_URL</code> に設定してください。
            </p>
          </div>
        )}
      </Card>
    </div>
  )
}
