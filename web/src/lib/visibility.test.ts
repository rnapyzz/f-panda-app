import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { Subject } from '../api/types.ts'
import { canOpenHistory, inputSubjects, seesAll } from './visibility.ts'

const subject = (id: number, is_restricted: boolean) => ({ id, is_restricted }) as Subject

test('閲覧制限: FP&A と経営陣はすべて見られ、現場は編集できる施策の履歴だけ', () => {
  assert.equal(seesAll('viewer'), true)
  assert.equal(seesAll('manager'), false)
  assert.equal(canOpenHistory('member', true), true)
  assert.equal(canOpenHistory('member', false), false)
})

test('閲覧制限のある科目は FP&A だけが入力できる', () => {
  const subjects = [subject(1, false), subject(2, true)]
  assert.deepEqual(inputSubjects(subjects, 'fpa_admin').map((s) => s.id), [1, 2])
  assert.deepEqual(inputSubjects(subjects, 'manager').map((s) => s.id), [1])
})
