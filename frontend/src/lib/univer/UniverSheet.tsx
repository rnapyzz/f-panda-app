import { LocaleType, merge, createUniver, type IWorkbookData } from '@univerjs/presets'
import { UniverSheetsCorePreset } from '@univerjs/preset-sheets-core'
import UniverPresetSheetsCoreJaJP from '@univerjs/preset-sheets-core/locales/ja-JP'
import '@univerjs/preset-sheets-core/lib/index.css'
import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react'
import type { FUniver } from '@univerjs/core/facade'
import type { FRange, FWorkbook } from '@univerjs/sheets/facade'

export interface SelectionSnapshot {
  sheetName: string
  startRow: number
  endRow: number
  startColumn: number
  endColumn: number
  /** display text for every cell in the range, row-major */
  values: string[][]
}

export interface UniverSheetHandle {
  /** Serialize the current workbook state for persistence (sheet_snapshot). */
  getSnapshot: () => IWorkbookData
  /** The currently selected range, or null if nothing is selected. */
  getSelection: () => SelectionSnapshot | null
}

interface Props {
  /** Previously saved sheet_snapshot to load, or omit to start a blank sheet. */
  initialSnapshot?: IWorkbookData
  className?: string
}

function toSelectionSnapshot(range: FRange): SelectionSnapshot {
  const ir = range.getRange()
  return {
    sheetName: range.getSheetName(),
    startRow: ir.startRow,
    endRow: ir.endRow,
    startColumn: ir.startColumn,
    endColumn: ir.endColumn,
    values: range.getDisplayValues(),
  }
}

export const UniverSheet = forwardRef<UniverSheetHandle, Props>(function UniverSheet(
  { initialSnapshot, className },
  ref,
) {
  const containerRef = useRef<HTMLDivElement>(null)
  const apiRef = useRef<FUniver | null>(null)
  const workbookRef = useRef<FWorkbook | null>(null)

  useEffect(() => {
    if (!containerRef.current) return

    const { univerAPI } = createUniver({
      locale: LocaleType.JA_JP,
      locales: {
        [LocaleType.JA_JP]: merge({}, UniverPresetSheetsCoreJaJP),
      },
      presets: [
        UniverSheetsCorePreset({
          container: containerRef.current,
        }),
      ],
    })
    apiRef.current = univerAPI

    const workbook = univerAPI.createWorkbook(initialSnapshot ?? { sheets: {}, sheetOrder: [] })
    workbookRef.current = workbook

    return () => {
      apiRef.current?.dispose()
      apiRef.current = null
      workbookRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useImperativeHandle(ref, () => ({
    getSnapshot: () => {
      if (!workbookRef.current) throw new Error('workbook not ready')
      return workbookRef.current.save()
    },
    getSelection: () => {
      const range = apiRef.current?.getActiveWorkbook()?.getActiveRange()
      if (!range) return null
      return toSelectionSnapshot(range)
    },
  }))

  return <div ref={containerRef} className={className} style={{ height: 480 }} />
})
