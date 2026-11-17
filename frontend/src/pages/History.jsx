import { useMemo, useState } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  Copy,
  Download,
  Eye,
  FileArchive,
  Folder,
  History as HistoryIcon,
  Link2,
  Loader2,
  Lock,
  Pin,
  PinOff,
  RefreshCw,
  RotateCcw,
  Search,
  ShieldCheck,
  Trash2,
  XCircle,
} from 'lucide-react'
import { api, formatBytes, formatDateTime, formatDuration } from '../api/client'
import { Badge, Button, Card, EmptyState, Input, Modal, Select, Switch } from '../components/ui'

function StatusBadge({ status }) {
  if (status === 'success') {
    return (
      <Badge variant="success">
        <CheckCircle2 className="h-3 w-3" />
        Success
      </Badge>
    )
  }
  if (status === 'failed') {
    return (
      <Badge variant="danger">
        <XCircle className="h-3 w-3" />
        Failed
      </Badge>
    )
  }
  return (
    <Badge variant="info">
      <Loader2 className="h-3 w-3 animate-spin" />
      Running
    </Badge>
  )
}

function parseJSONList(raw) {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export function History({ backups, loading, onRefresh, notify }) {
  const [statusFilter, setStatusFilter] = useState('')
  const [search, setSearch] = useState('')

  const [inspectItem, setInspectItem] = useState(null)
  const [inspectLoading, setInspectLoading] = useState(false)
  const [inspectData, setInspectData] = useState(null)
  const [verifyLoading, setVerifyLoading] = useState(false)
  const [verifyResult, setVerifyResult] = useState(null)

  const [restoreItem, setRestoreItem] = useState(null)
  const [restorePause, setRestorePause] = useState(true)
  const [restoring, setRestoring] = useState(false)

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return backups.filter((b) => {
      if (statusFilter && b.status !== statusFilter) return false
      if (!q) return true
      return (
        b.targetName.toLowerCase().includes(q) ||
        (b.fileName || '').toLowerCase().includes(q) ||
        (b.storageProviderName || '').toLowerCase().includes(q)
      )
    })
  }, [backups, statusFilter, search])

  async function handleDelete(item) {
    if (!window.confirm(`Delete backup record${item.fileName ? ` and archive ${item.fileName}` : ''}?`)) {
      return
    }
    try {
      await api.deleteBackup(item.id, true)
      notify('success', 'Backup removed')
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    }
  }

  async function handleCopySas(item) {
    try {
      const res = await api.createBackupSasLink(item.id)
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(res.url)
        notify('success', 'Copied 1-hour Azure SAS download URL to clipboard')
      } else {
        notify('info', res.url)
      }
    } catch (err) {
      notify('error', err.message)
    }
  }

  async function handleCopyChecksum(checksum) {
    if (!checksum) return
    try {
      await navigator.clipboard?.writeText(checksum)
      notify('success', 'SHA-256 checksum copied')
    } catch {
      notify('info', checksum)
    }
  }

  async function handleTogglePin(item) {
    try {
      const res = await api.togglePinBackup(item.id)
      notify('success', res.backup.pinned ? 'Backup pinned (protected from pruning)' : 'Backup unpinned')
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    }
  }

  async function openInspect(item) {
    setInspectItem(item)
    setInspectData(null)
    setVerifyResult(null)
    if (item.status !== 'success') return

    setInspectLoading(true)
    try {
      const res = await api.inspectBackup(item.id)
      setInspectData(res)
    } catch (err) {
      notify('error', err.message)
    } finally {
      setInspectLoading(false)
    }
  }

  async function handleVerify() {
    if (!inspectItem) return
    setVerifyLoading(true)
    try {
      const res = await api.verifyBackup(inspectItem.id)
      setVerifyResult(res)
      notify(
        res.match ? 'success' : 'error',
        res.match ? 'SHA-256 integrity verified' : 'Checksum mismatch detected!',
      )
    } catch (err) {
      notify('error', err.message)
    } finally {
      setVerifyLoading(false)
    }
  }

  async function handleRestore(e) {
    e.preventDefault()
    if (!restoreItem) return
    setRestoring(true)
    try {
      const res = await api.restoreBackup(restoreItem.id, restorePause)
      notify(
        'success',
        `Restored ${res.restoredMounts} mount(s) for ${res.targetName} in ${formatDuration(res.durationMs)}`,
      )
      setRestoreItem(null)
    } catch (err) {
      notify('error', `Restore failed: ${err.message}`)
    } finally {
      setRestoring(false)
    }
  }

  return (
    <div className="space-y-5">
      <Card className="p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <div className="relative w-64">
              <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-zinc-500" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search target or filename..."
                className="pl-9 h-9"
              />
            </div>

            <Select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value)}
              className="w-44 h-9"
            >
              <option value="">All Statuses</option>
              <option value="success">Success</option>
              <option value="running">Running</option>
              <option value="failed">Failed</option>
            </Select>
          </div>

          <Button variant="outline" size="sm" onClick={onRefresh} disabled={loading}>
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
        </div>
      </Card>

      {filtered.length === 0 ? (
        <EmptyState
          icon={HistoryIcon}
          title="No backup logs found"
          description="Executed backups and their downloadable .tar.gz archives appear here."
        />
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-zinc-800 bg-zinc-950/70 text-xs uppercase tracking-wider text-zinc-400">
                <tr>
                  <th className="px-4 py-3 font-medium">Target</th>
                  <th className="px-4 py-3 font-medium">Status</th>
                  <th className="px-4 py-3 font-medium">Archive &amp; Checksum</th>
                  <th className="px-4 py-3 font-medium">Storage</th>
                  <th className="px-4 py-3 font-medium">Duration &amp; Time</th>
                  <th className="px-4 py-3 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800/70">
                {filtered.map((b) => (
                  <tr key={b.id} className="hover:bg-zinc-900/50">
                    <td className="px-4 py-3.5 align-top">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-zinc-100">{b.targetName}</span>
                        <Badge variant="neutral">{b.targetType}</Badge>
                      </div>
                      <div className="mt-0.5 text-xs text-zinc-500">
                        {b.trigger} · {b.volumeCount} mount{b.volumeCount === 1 ? '' : 's'}
                      </div>
                    </td>

                    <td className="px-4 py-3.5 align-top">
                      <StatusBadge status={b.status} />
                      {b.errorMessage && (
                        <p className="mt-1 max-w-xs text-xs text-rose-400 break-words">
                          {b.errorMessage}
                        </p>
                      )}
                    </td>

                    <td className="px-4 py-3.5 align-top">
                      {b.fileName ? (
                        <div>
                          <div className="flex items-center gap-1.5 font-mono text-xs text-zinc-200">
                            <span>{b.fileName}</span>
                            {b.encrypted && (
                              <Badge variant="info" className="text-[10px] py-0 px-1.5">
                                <Lock className="h-2.5 w-2.5" />
                                AES-256
                              </Badge>
                            )}
                            {b.pinned && (
                              <Badge variant="warning" className="text-[10px] py-0 px-1.5">
                                <Pin className="h-2.5 w-2.5" />
                                Pinned
                              </Badge>
                            )}
                          </div>
                          <div className="mt-0.5 flex items-center gap-2 text-xs text-zinc-400">
                            <span>{formatBytes(b.sizeBytes)}</span>
                            {b.checksumSha256 && (
                              <button
                                type="button"
                                onClick={() => handleCopyChecksum(b.checksumSha256)}
                                title="Copy full SHA-256 checksum"
                                className="inline-flex items-center gap-1 font-mono text-[11px] text-zinc-500 hover:text-zinc-200 cursor-pointer"
                              >
                                sha256:{b.checksumSha256.slice(0, 12)}…
                                <Copy className="h-3 w-3" />
                              </button>
                            )}
                          </div>
                        </div>
                      ) : (
                        <span className="text-xs text-zinc-500">—</span>
                      )}
                    </td>

                    <td className="px-4 py-3.5 align-top">
                      <div className="text-xs text-zinc-200">{b.storageProviderName}</div>
                      <div className="mt-0.5 flex items-center gap-1.5 text-[11px] uppercase text-zinc-500">
                        <span>{b.storageType}</span>
                        {b.accessTier && (
                          <Badge variant="info" className="text-[10px] py-0 px-1.5 normal-case">
                            {b.accessTier}
                          </Badge>
                        )}
                      </div>
                    </td>

                    <td className="px-4 py-3.5 align-top">
                      <div className="text-xs text-zinc-200">{formatDateTime(b.startedAt)}</div>
                      <div className="mt-0.5 text-xs text-zinc-500">
                        {formatDuration(b.durationMs)}
                      </div>
                    </td>

                    <td className="px-4 py-3.5 align-top text-right">
                      <div className="inline-flex flex-wrap justify-end items-center gap-1.5">
                        {b.status === 'success' && (
                          <>
                            <Button
                              variant="ghost"
                              size="sm"
                              title={b.pinned ? 'Unpin backup' : 'Pin backup to protect from retention pruning'}
                              onClick={() => handleTogglePin(b)}
                            >
                              {b.pinned ? (
                                <PinOff className="h-3.5 w-3.5 text-amber-400" />
                              ) : (
                                <Pin className="h-3.5 w-3.5" />
                              )}
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              title="Inspect archive contents & verify integrity"
                              onClick={() => openInspect(b)}
                            >
                              <Eye className="h-3.5 w-3.5" />
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              title="Restore volumes from this archive"
                              onClick={() => {
                                setRestorePause(true)
                                setRestoreItem(b)
                              }}
                            >
                              <RotateCcw className="h-3.5 w-3.5" />
                              Restore
                            </Button>
                          </>
                        )}
                        {b.status === 'success' && b.storageType === 'azure' && (
                          <Button
                            variant="outline"
                            size="sm"
                            title="Copy 1-hour pre-signed SAS download URL"
                            onClick={() => handleCopySas(b)}
                          >
                            <Link2 className="h-3.5 w-3.5" />
                            SAS
                          </Button>
                        )}
                        {b.status === 'success' && (
                          <a
                            href={api.downloadBackupUrl(b.id)}
                            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-zinc-700 px-2.5 text-xs text-zinc-200 hover:bg-zinc-800"
                          >
                            <Download className="h-3.5 w-3.5" />
                            Download
                          </a>
                        )}
                        <Button
                          variant="danger"
                          size="sm"
                          disabled={b.status === 'running'}
                          onClick={() => handleDelete(b)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {/* Inspect / Details Modal */}
      <Modal
        open={Boolean(inspectItem)}
        onClose={() => setInspectItem(null)}
        title="Backup Archive Details"
        subtitle={inspectItem?.fileName || inspectItem?.targetName}
        maxWidth="max-w-2xl"
      >
        {inspectItem && (
          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
                <div className="text-zinc-500">Mounted Sources</div>
                <div className="mt-1.5 space-y-1">
                  {parseJSONList(inspectItem.volumesJson).map((m, idx) => (
                    <div key={idx} className="truncate font-mono text-zinc-200">
                      {m.archiveDir}/ ← {m.name || m.source}
                    </div>
                  ))}
                </div>
              </div>
              <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
                <div className="text-zinc-500">Paused Containers</div>
                <div className="mt-1.5 text-zinc-200">
                  {parseJSONList(inspectItem.pausedContainersJson).length > 0
                    ? parseJSONList(inspectItem.pausedContainersJson).join(', ')
                    : 'None paused'}
                </div>
                {inspectItem.checksumSha256 && (
                  <div className="mt-2 pt-2 border-t border-zinc-800/80">
                    <div className="text-zinc-500">SHA-256</div>
                    <div className="mt-0.5 break-all font-mono text-[11px] text-zinc-300">
                      {inspectItem.checksumSha256}
                    </div>
                  </div>
                )}
              </div>
            </div>

            <div className="flex items-center justify-between">
              <div className="text-xs font-medium text-zinc-300">
                Archive Contents
                {inspectData ? ` (${inspectData.totalEntries} entries${inspectData.truncated ? ', showing first 500' : ''})` : ''}
              </div>
              <Button variant="outline" size="sm" onClick={handleVerify} disabled={verifyLoading}>
                <ShieldCheck className="h-3.5 w-3.5" />
                {verifyLoading ? 'Verifying...' : 'Verify SHA-256'}
              </Button>
            </div>

            {verifyResult && (
              <div
                className={`rounded-xl border px-3 py-2 text-xs ${
                  verifyResult.match
                    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                    : 'border-rose-500/30 bg-rose-500/10 text-rose-300'
                }`}
              >
                {verifyResult.match
                  ? `Integrity verified (${formatBytes(verifyResult.bytesRead)} read, checksum matches).`
                  : `Checksum mismatch! Expected ${verifyResult.expected}, got ${verifyResult.actual}.`}
              </div>
            )}

            <div className="max-h-64 overflow-y-auto rounded-xl border border-zinc-800 bg-zinc-950 p-2 font-mono text-xs">
              {inspectLoading ? (
                <div className="flex items-center justify-center py-8 text-zinc-400">
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Reading archive entries...
                </div>
              ) : !inspectData?.entries?.length ? (
                <div className="py-6 text-center text-zinc-500">No entries returned.</div>
              ) : (
                <div className="space-y-0.5">
                  {inspectData.entries.map((entry, idx) => (
                    <div
                      key={`${entry.name}-${idx}`}
                      className="flex items-center justify-between rounded px-2 py-1 hover:bg-zinc-900"
                    >
                      <span className="flex items-center gap-2 truncate text-zinc-200">
                        {entry.isDir ? (
                          <Folder className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                        ) : (
                          <FileArchive className="h-3.5 w-3.5 shrink-0 text-sky-400" />
                        )}
                        <span className="truncate">{entry.name}</span>
                      </span>
                      <span className="ml-3 flex items-center gap-2 shrink-0 text-[11px] text-zinc-500">
                        <span>{entry.isDir ? 'dir' : formatBytes(entry.size)}</span>
                        {!entry.isDir && (
                          <a
                            href={api.extractBackupFileUrl(inspectItem.id, entry.name)}
                            title="Download this file"
                            className="rounded border border-zinc-700 px-1.5 py-0.5 text-[10px] text-zinc-200 hover:bg-zinc-800"
                          >
                            Get
                          </a>
                        )}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </Modal>

      {/* Safe Restore Modal */}
      <Modal
        open={Boolean(restoreItem)}
        onClose={() => setRestoreItem(null)}
        title="Restore Backup Archive"
        subtitle={restoreItem ? `Target: ${restoreItem.targetName} (${restoreItem.fileName})` : ''}
      >
        {restoreItem && (
          <form onSubmit={handleRestore} className="space-y-4">
            <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-200 flex items-start gap-2.5">
              <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5 text-amber-400" />
              <div>
                Restoring extracts <span className="font-mono">{restoreItem.fileName}</span> directly into
                the original Docker volume(s) / bind mounts, overwriting existing files with matching paths.
              </div>
            </div>

            <div className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3 text-xs space-y-1">
              <div className="text-zinc-400 mb-1">Volumes to restore:</div>
              {parseJSONList(restoreItem.volumesJson).map((m, idx) => (
                <div key={idx} className="font-mono text-zinc-200 truncate">
                  {m.name || m.source} → {m.destination}
                </div>
              ))}
            </div>

            <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
              <span className="text-xs text-zinc-200">
                Pause running target container(s) during restore
              </span>
              <Switch checked={restorePause} onChange={setRestorePause} />
            </div>

            <div className="flex items-center justify-end gap-2 pt-2">
              <Button type="button" variant="ghost" onClick={() => setRestoreItem(null)}>
                Cancel
              </Button>
              <Button type="submit" disabled={restoring}>
                <RotateCcw className="h-4 w-4" />
                {restoring ? 'Restoring...' : 'Confirm & Restore'}
              </Button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  )
}
