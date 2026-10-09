// 画面の図を画像（PNG）にする（docs/plan.md「2.23」）。
// 要素を複製して見た目（計算済みのスタイル）を書き込み、SVG の foreignObject に入れて canvas に描く。
// data-no-capture の付いた要素（ボタンなど）は画像に含めない。Chrome・Edge で動く。描けないブラウザでは例外を投げる。

const scale = 2
const padding = 24
const titleSize = 16

/** 計算済みのスタイルを、複製した要素の style に書き込む（子孫も） */
function inlineStyles(source: Element, clone: Element) {
  const computed = getComputedStyle(source)
  const style = (clone as HTMLElement | SVGElement).style
  if (style) {
    for (let i = 0; i < computed.length; i++) {
      const name = computed[i]
      style.setProperty(name, computed.getPropertyValue(name), computed.getPropertyPriority(name))
    }
  }
  const sourceChildren = Array.from(source.children)
  const cloneChildren = Array.from(clone.children)
  sourceChildren.forEach((child, i) => {
    if (cloneChildren[i]) inlineStyles(child, cloneChildren[i])
  })
}

function removeExcluded(clone: Element) {
  clone.querySelectorAll('[data-no-capture]').forEach((el) => el.remove())
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = () => reject(new Error('図を画像にできませんでした'))
    img.src = src
  })
}

/** 要素を PNG（2倍の解像度、白い背景、上に題）にして Blob で返す */
export async function elementToPng(el: HTMLElement, title?: string): Promise<Blob> {
  const rect = el.getBoundingClientRect()
  const width = Math.ceil(rect.width)
  const height = Math.ceil(rect.height)
  const clone = el.cloneNode(true) as HTMLElement
  inlineStyles(el, clone)
  removeExcluded(clone)
  clone.style.margin = '0'
  clone.style.width = `${width}px`
  // 入力欄の値は属性にしないと複製に残らない
  clone.querySelectorAll('input, select, textarea').forEach((input) => input.remove())

  const html = new XMLSerializer().serializeToString(clone)
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}">` +
    `<foreignObject x="0" y="0" width="100%" height="100%">${html}</foreignObject></svg>`
  const img = await loadImage(`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`)

  const top = title ? padding + titleSize + 12 : padding
  const canvas = document.createElement('canvas')
  canvas.width = (width + padding * 2) * scale
  canvas.height = (height + top + padding) * scale
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('図を画像にできませんでした')
  ctx.scale(scale, scale)
  ctx.fillStyle = '#ffffff'
  ctx.fillRect(0, 0, width + padding * 2, height + top + padding)
  if (title) {
    ctx.fillStyle = '#0f172a'
    ctx.font = `600 ${titleSize}px system-ui, -apple-system, "Hiragino Sans", "Yu Gothic UI", sans-serif`
    ctx.textBaseline = 'top'
    ctx.fillText(title, padding, padding)
  }
  ctx.drawImage(img, padding, top, width, height)
  return new Promise((resolve, reject) => {
    try {
      canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('図を画像にできませんでした'))), 'image/png')
    } catch {
      // Safari などは foreignObject を描いた canvas を書き出せない
      reject(new Error('このブラウザでは図を画像にできません。Chrome か Edge をお使いください'))
    }
  })
}

/** ファイル名に使えない文字を除く */
export function safeFileName(name: string): string {
  return name.replace(/[\\/:*?"<>|]/g, '_').trim()
}

/** 要素を PNG で保存する */
export async function saveElementAsPng(el: HTMLElement, fileName: string, title?: string): Promise<void> {
  const blob = await elementToPng(el, title)
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${safeFileName(fileName)}.png`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
