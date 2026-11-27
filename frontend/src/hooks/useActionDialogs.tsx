import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ConfirmDialog, PromptDialog } from '../components/ActionDialogs'

export interface ConfirmOptions {
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
}

export interface PromptOptions {
  title: string
  message?: string
  defaultValue?: string
  placeholder?: string
  presets?: string[]
  validate?: (val: string) => string | null
  confirmLabel?: string
  cancelLabel?: string
}

export function useActionDialogs() {
  const [confirmReq, setConfirmReq] = useState<ConfirmOptions | null>(null)
  const [promptReq, setPromptReq] = useState<PromptOptions | null>(null)
  const confirmResolveRef = useRef<((ok: boolean) => void) | null>(null)
  const promptResolveRef = useRef<((val: string | null) => void) | null>(null)

  useEffect(() => {
    return () => {
      confirmResolveRef.current?.(false)
      confirmResolveRef.current = null
      promptResolveRef.current?.(null)
      promptResolveRef.current = null
    }
  }, [])

  const confirm = useCallback((opts: ConfirmOptions): Promise<boolean> => {
    if (confirmResolveRef.current) confirmResolveRef.current(false)
    return new Promise((resolve) => {
      confirmResolveRef.current = resolve
      setConfirmReq(opts)
    })
  }, [])

  const prompt = useCallback((opts: PromptOptions): Promise<string | null> => {
    if (promptResolveRef.current) promptResolveRef.current(null)
    return new Promise((resolve) => {
      promptResolveRef.current = resolve
      setPromptReq(opts)
    })
  }, [])

  const settleConfirm = useCallback((ok: boolean) => {
    const res = confirmResolveRef.current
    confirmResolveRef.current = null
    setConfirmReq(null)
    res?.(ok)
  }, [])

  const settlePrompt = useCallback((val: string | null) => {
    const res = promptResolveRef.current
    promptResolveRef.current = null
    setPromptReq(null)
    res?.(val)
  }, [])

  const dialogNode = useMemo(
    () => (
      <>
        {confirmReq && (
          <ConfirmDialog
            open
            title={confirmReq.title}
            message={confirmReq.message}
            confirmLabel={confirmReq.confirmLabel}
            cancelLabel={confirmReq.cancelLabel}
            danger={confirmReq.danger}
            onConfirm={() => settleConfirm(true)}
            onCancel={() => settleConfirm(false)}
          />
        )}
        {promptReq && (
          <PromptDialog
            open
            title={promptReq.title}
            message={promptReq.message}
            defaultValue={promptReq.defaultValue}
            placeholder={promptReq.placeholder}
            presets={promptReq.presets}
            validate={promptReq.validate}
            confirmLabel={promptReq.confirmLabel}
            cancelLabel={promptReq.cancelLabel}
            onConfirm={(v) => settlePrompt(v)}
            onCancel={() => settlePrompt(null)}
          />
        )}
      </>
    ),
    [confirmReq, promptReq, settleConfirm, settlePrompt]
  )

  return { confirm, prompt, dialogNode }
}
