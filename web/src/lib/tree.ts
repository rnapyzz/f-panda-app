// 階層マスタ（組織・セグメント）の表示用ヘルパー。

import type { TreeNode } from '../api/types'

export type Tree = {
  /** 親子順（深さ優先）に並べたノードと深さ */
  ordered: { node: TreeNode; depth: number }[]
  byId: Map<number, TreeNode>
  children: Map<number | null, TreeNode[]>
}

export function buildTree(nodes: TreeNode[]): Tree {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const children = new Map<number | null, TreeNode[]>()
  for (const n of nodes) {
    const key = n.parent_id
    if (!children.has(key)) children.set(key, [])
    children.get(key)!.push(n)
  }
  for (const list of children.values()) {
    list.sort((a, b) => a.sort_order - b.sort_order || a.id - b.id)
  }
  const ordered: Tree['ordered'] = []
  const walk = (parent: number | null, depth: number) => {
    for (const n of children.get(parent) ?? []) {
      ordered.push({ node: n, depth })
      walk(n.id, depth + 1)
    }
  }
  walk(null, 0)
  return { ordered, byId, children }
}

/** ルートからのパス名（例: "AAA本部 / BBB部"） */
export function pathName(tree: Tree, id: number | null | undefined): string {
  const names: string[] = []
  let cur = id != null ? tree.byId.get(id) : undefined
  while (cur) {
    names.unshift(cur.name)
    cur = cur.parent_id != null ? tree.byId.get(cur.parent_id) : undefined
  }
  return names.join(' / ')
}

export function isLeaf(tree: Tree, id: number): boolean {
  return (tree.children.get(id) ?? []).length === 0
}

/** id 自身と配下のノード ID */
export function subtreeIds(tree: Tree, id: number): Set<number> {
  const out = new Set<number>([id])
  const walk = (p: number) => {
    for (const c of tree.children.get(p) ?? []) {
      out.add(c.id)
      walk(c.id)
    }
  }
  walk(id)
  return out
}
