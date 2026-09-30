import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { TreeNode } from '../api/types'
import { buildTree, isLeaf, pathName, subtreeIds } from './tree.ts'

const node = (id: number, parent_id: number | null, name: string, sort_order = 0): TreeNode => ({
  id,
  parent_id,
  code: `N-${id}`,
  name,
  level: 0,
  sort_order,
  created_at: '',
  updated_at: '',
})

// 本部(1) ─ 第二部(3, 表示順1) ─ 課(4)
//        └ 第一部(2, 表示順0)
// 別本部(5)
const nodes = [node(5, null, '別本部', 1), node(3, 1, '第二部', 1), node(1, null, '本部', 0), node(4, 3, '課'), node(2, 1, '第一部', 0)]

test('buildTree: 表示順で深さ優先に並べる', () => {
  const t = buildTree(nodes)
  assert.deepEqual(
    t.ordered.map(({ node, depth }) => `${'-'.repeat(depth)}${node.name}`),
    ['本部', '-第一部', '-第二部', '--課', '別本部'],
  )
})

test('pathName / isLeaf / subtreeIds', () => {
  const t = buildTree(nodes)
  assert.equal(pathName(t, 4), '本部 / 第二部 / 課')
  assert.equal(pathName(t, null), '')
  assert.equal(isLeaf(t, 4), true)
  assert.equal(isLeaf(t, 3), false)
  assert.deepEqual([...subtreeIds(t, 1)].sort(), [1, 2, 3, 4])
})
