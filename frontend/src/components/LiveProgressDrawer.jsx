import { Activity, CheckCircle2, Loader2, MinusCircle, Terminal, X, XCircle } from 'lucide-react'
import { formatBytes, formatBytesPerSec, formatDuration } from '../api/client'
import { Badge, Modal } from './ui'

export function StepStatusIcon({ status }) {
  if (status === 'done') {
    return <CheckCircle2 className="h-4 w-4 text-emerald-400 shrink-0" />
  }
  if (status === 'failed') {
    return <XCircle className="h-4 w-4 text-rose-400 shrink-0" />
  }
  if (status === 'skipped') {
    return <MinusCircle className="h-4 w-4 text-zinc-500 shrink-0" />
  }
  if (status === 'running') {
    return <Loader2 className="h-4 w-4 text-sky-400 animate-spin shrink-0" />
  }
  return <span className="h-2 w-2 rounded-full bg-zinc-600 shrink-0 mx-1" />
}

export function JobStepsList({ steps = [] }) {
  if (!steps.length) return null
  return (
    <div className="space-y-1.5">
      {steps.map((step, idx) => (
        <div
          key={`${step.name}-${idx}`}
          className="flex items-start justify-between gap-3 rounded-lg border border-zinc-800/80 bg-zinc-950/60 px-3 py-2 text-xs"
        >
          <div className="flex items-start gap-2 min-w-0">
            <div className="mt-0.5">
              <StepStatusIcon status={step.status} />
            </div>
            <div className="min-w-0">
              <div
                className={`font-medium ${
                  step.status === 'skipped' ? 'text-zinc-400' : 'text-zinc-100'
                }`}
              >
                {step.name}
              </div>
              {step.detail && (
                <div className="mt-0.5 break-words text-[11px] text-zinc-400">{step.detail}</div>
              )}
            </div>
          </div>
          <div className="shrink-0 flex items-center gap-2">
            {step.durationMs > 0 && (
              <span className="font-mono text-[11px] text-zinc-400">
                {formatDuration(step.durationMs)}
              </span>
            )}
            <Badge
              variant={
                step.status === 'done'
                  ? 'success'
                  : step.status === 'failed'
                    ? 'danger'
                    : step.status === 'running'
                      ? 'info'
                      : 'neutral'
              }
              className="text-[10px] py-0 px-1.5 capitalize"
            >
              {step.status}
            </Badge>
          </div>
        </div>
      ))}
    </div>
  )
}

export function LiveProgressDrawer({ open, onClose, jobs = [] }) {
  const sorted = [...jobs].sort((a, b) => Number(b.backupId) - Number(a.backupId))
  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Live Backup Activity"
      subtitle="Real-time step timeline, streamed bytes, and throughput"
      maxWidth="max-w-2xl"
    >
      {sorted.length === 0 ? (
        <div className="py-8 text-center text-xs text-zinc-400">
          No active or recent live backup jobs. Start a backup to watch live step progress here.
        </div>
      ) : (
        <div className="max-h-[70vh] space-y-4 overflow-y-auto pr-1">
          {sorted.map((job) => (
            <div
              key={job.backupId}
              className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-4 space-y-3"
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <Activity
                    className={`h-4 w-4 ${
                      job.status === 'running'
                        ? 'text-sky-400 animate-pulse'
                        : job.status === 'success'
                          ? 'text-emerald-400'
                          : 'text-rose-400'
                    }`}
                  />
                  <span className="text-sm font-semibold text-zinc-100">{job.targetName}</span>
                  <Badge variant="neutral">{job.targetType}</Badge>
                  <Badge
                    variant={
                      job.status === 'running'
                        ? 'info'
                        : job.status === 'success'
                          ? 'success'
                          : 'danger'
                    }
                  >
                    {job.phaseLabel || job.status}
                  </Badge>
                </div>
                <div className="flex items-center gap-3 font-mono text-xs text-zinc-300">
                  <span>{formatBytes(job.bytesProcessed)}</span>
                  {job.status === 'running' && job.bytesPerSec > 0 && (
                    <span className="text-sky-400">{formatBytesPerSec(job.bytesPerSec)}</span>
                  )}
                </div>
              </div>

              {job.error && (
                <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs text-rose-300">
                  {job.error}
                </div>
              )}

              <JobStepsList steps={job.steps || []} />

              {job.logs?.length > 0 && (
                <details className="rounded-lg border border-zinc-800/80 bg-zinc-950 p-2.5 text-[11px]">
                  <summary className="flex items-center gap-1.5 cursor-pointer text-zinc-400 hover:text-zinc-200">
                    <Terminal className="h-3.5 w-3.5" />
                    Execution Log ({job.logs.length} lines)
                  </summary>
                  <pre className="mt-2 max-h-40 overflow-y-auto font-mono text-[11px] leading-relaxed text-zinc-300 whitespace-pre-wrap">
                    {job.logs.join('\n')}
                  </pre>
                </details>
              )}
            </div>
          ))}
          <div className="flex justify-end">
            <button
              type="button"
              onClick={onClose}
              className="inline-flex items-center gap-1 text-xs text-zinc-400 hover:text-zinc-200 cursor-pointer"
            >
              <X className="h-3.5 w-3.5" />
              Close drawer
            </button>
          </div>
        </div>
      )}
    </Modal>
  )
}
