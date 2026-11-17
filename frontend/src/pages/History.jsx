import { useMemo, useState } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  GitCompare,
  StickyNote,
  X,
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
import { JobStepsList } from '../components/LiveProgressDrawer'
import { Badge, Button, Card, EmptyState, Input, Label, Modal, Select, Switch } from '../components/ui'

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
  const [inspectSteps, setInspectSteps] = useState([])
  const [verifyLoading, setVerifyLoading] = useState(false)
  const [verifyResult, setVerifyResult] = useState(null)

  const [restoreItem, setRestoreItem] = useState(null)
  const [restoreMode, setRestoreMode] = useState('inplace') // 'inplace' | 'staging'
  const [restoreVolumeName, setRestoreVolumeName] = useState('')
  const [restorePreSnap, setRestorePreSnap] = useState(true)
  const [restorePause, setRestorePause] = useState(true)
  const [restoring, setRestoring] = useState(false)
  const [restorePaths, setRestorePaths] = useState([])
  const [inspectSelection, setInspectSelection] = useState([])

  const [selectedIds, setSelectedIds] = useState([])
  const [bulkBusy, setBulkBusy] = useState('')

  const [noteItem, setNoteItem] = useState(null)
  const [noteDraft, setNoteDraft] = useState('')
  const [savingNote, setSavingNote] = useState(false)

  const [compareData, setCompareData] = useState(null)
  const [compareLoading, setCompareLoading] = useState(false)

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return backups.filter((b) => {
      if (statusFilter && b.status !== statusFilter) return false
      if (!q) return true
      return (
        b.targetName.toLowerCase().includes(q) ||
        (b.fileName || '').toLowerCase().includes(q) ||
        (b.storageProviderName || '').toLowerCase().includes(q) ||
        (b.note || '').toLowerCase().includes(q)
      )
    })
  }, [backups, statusFilter, search])

  // Drop selections for rows that disappeared after a refresh or filter change.
  const visibleSelected = useMemo(() => {
    const visible = new Set(filtered.map((b) => b.id))
    return selectedIds.filter((id) => visible.has(id))
  }, [filtered, selectedIds])
  const selectedBackups = filtered.filter((b) => visibleSelected.includes(b.id))
  const selectableRows = filtered.filter((b) => b.status !== 'running')
  const allSelected = selectableRows.length > 0 && visibleSelected.length === selectableRows.length
  const canCompare =
    selectedBackups.length === 2 && selectedBackups.every((b) => b.status === 'success')

  function toggleSelected(id) {
    setSelectedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
  }

  function toggleSelectAll() {
    setSelectedIds(allSelected ? [] : selectableRows.map((b) => b.id))
  }

  async function runBulk(action) {
    const ids =
      action === 'delete'
        ? visibleSelected
        : selectedBackups.filter((b) => b.status === 'success').map((b) => b.id)
    if (ids.length === 0) {
      notify('error', 'Select at least one successful backup for this action')
      return
    }
    if (
      action === 'delete' &&
      !window.confirm(`Delete ${ids.length} backup record(s) and their archive files? This cannot be undone.`)
    ) {
      return
    }
    setBulkBusy(action)
    try {
      const res = await api.bulkBackupAction(action, ids)
      const verb = { pin: 'Pinned', unpin: 'Unpinned', delete: 'Deleted', verify: 'Verified' }[action]
      if (res.failed > 0) {
        const firstErr = res.results.find((r) => !r.ok)
        notify('error', `${verb} ${res.succeeded}/${ids.length} · ${res.failed} failed (#${firstErr.id}: ${firstErr.error})`)
      } else {
        notify('success', `${verb} ${res.succeeded} backup(s)`)
      }
      if (action === 'delete') setSelectedIds([])
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    } finally {
      setBulkBusy('')
    }
  }

  async function handleCompare() {
    if (!canCompare) return
    const [a, b] = [...selectedBackups].sort((x, y) => new Date(x.startedAt) - new Date(y.startedAt))
    setCompareLoading(true)
    setCompareData({ loading: true, base: a, target: b })
    try {
      const res = await api.compareBackups(a.id, b.id)
      setCompareData(res)
    } catch (err) {
      setCompareData(null)
      notify('error', `Compare failed: ${err.message}`)
    } finally {
      setCompareLoading(false)
    }
  }

  function openNote(item) {
    setNoteItem(item)
    setNoteDraft(item.note || '')
  }

  async function handleSaveNote(e) {
    e.preventDefault()
    if (!noteItem) return
    setSavingNote(true)
    try {
      await api.updateBackupNote(noteItem.id, noteDraft.trim())
      notify('success', noteDraft.trim() ? 'Note saved' : 'Note removed')
      setNoteItem(null)
      onRefresh()
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSavingNote(false)
    }
  }

  function openRestore(item, paths = []) {
    const slug = (item.targetName || 'backup').toLowerCase().replace(/[^a-z0-9_-]+/g, '-')
    setRestoreMode('inplace')
    setRestoreVolumeName(`${slug}-restored`)
    setRestorePreSnap(true)
    setRestorePause(true)
    setRestorePaths(paths)
    setRestoreItem(item)
  }

  function toggleInspectPath(name) {
    setInspectSelection((prev) => (prev.includes(name) ? prev.filter((p) => p !== name) : [...prev, name]))
  }

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
    setInspectSelection([])
    setInspectSteps(parseJSONList(item.stepsJson))
    setVerifyResult(null)
    if (item.status !== 'success') return

    setInspectLoading(true)
    try {
      const [res, progRes] = await Promise.all([
        api.inspectBackup(item.id),
        api.getBackupProgress(item.id).catch(() => null),
      ])
      setInspectData(res)
      if (progRes?.job?.steps?.length) {
        setInspectSteps(progRes.job.steps)
      }
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
    if (restoreMode === 'staging' && !restoreVolumeName.trim()) {
      notify('error', 'Enter a staging volume name')
      return
    }
    setRestoring(true)
    try {
      const res = await api.restoreBackup(restoreItem.id, {
        pauseContainer: restoreMode === 'inplace' ? restorePause : false,
        targetVolumeName: restoreMode === 'staging' ? restoreVolumeName.trim() : '',
        preRestoreBackup: restoreMode === 'inplace' ? restorePreSnap : false,
        paths: restorePaths,
      })
      const pathInfo = res.restoredPaths ? `${res.restoredPaths} selected path(s) from ` : ''
      if (res.restoredVolume) {
        notify(
          'success',
          `Extracted ${pathInfo}archive into staging volume "${res.restoredVolume}" in ${formatDuration(res.durationMs)}`,
        )
      } else {
        notify(
          'success',
          `Restored ${pathInfo || `${res.restoredMounts} mount(s) for `}${res.targetName} in ${formatDuration(res.durationMs)}${
            res.preRestoreBackupId ? ' (safety snapshot saved)' : ''
          }`,
        )
      }
      setRestoreItem(null)
      setInspectItem(null)
      onRefresh()
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

      {visibleSelected.length > 0 && (
        <Card className="p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-xs text-zinc-300">
              <span className="font-semibold text-zinc-100">{visibleSelected.length}</span> selected
            </span>
            <div className="flex flex-wrap items-center gap-1.5">
              <Button variant="outline" size="sm" disabled={Boolean(bulkBusy)} onClick={() => runBulk('pin')}>
                <Pin className="h-3.5 w-3.5" />
                Pin
              </Button>
              <Button variant="outline" size="sm" disabled={Boolean(bulkBusy)} onClick={() => runBulk('unpin')}>
                <PinOff className="h-3.5 w-3.5" />
                Unpin
              </Button>
              <Button variant="outline" size="sm" disabled={Boolean(bulkBusy)} onClick={() => runBulk('verify')}>
                <ShieldCheck className="h-3.5 w-3.5" />
                {bulkBusy === 'verify' ? 'Verifying...' : 'Verify'}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={!canCompare || compareLoading}
                title={canCompare ? 'Compare file listings of the two selected backups' : 'Select exactly two successful backups to compare'}
                onClick={handleCompare}
              >
                <GitCompare className="h-3.5 w-3.5" />
                Compare
              </Button>
              <Button variant="danger" size="sm" disabled={Boolean(bulkBusy)} onClick={() => runBulk('delete')}>
                <Trash2 className="h-3.5 w-3.5" />
                Delete
              </Button>
              <Button variant="ghost" size="sm" title="Clear selection" onClick={() => setSelectedIds([])}>
                <X className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </Card>
      )}

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
                  <th className="w-10 pl-4 py-3">
                    <input
                      type="checkbox"
                      aria-label="Select all backups"
                      checked={allSelected}
                      onChange={toggleSelectAll}
                      className="h-3.5 w-3.5 accent-sky-500 cursor-pointer"
                    />
                  </th>
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
                  <tr
                    key={b.id}
                    className={visibleSelected.includes(b.id) ? 'bg-zinc-900/60' : 'hover:bg-zinc-900/50'}
                  >
                    <td className="w-10 pl-4 py-3.5 align-top">
                      <input
                        type="checkbox"
                        aria-label={`Select backup ${b.id}`}
                        disabled={b.status === 'running'}
                        checked={visibleSelected.includes(b.id)}
                        onChange={() => toggleSelected(b.id)}
                        className="mt-1 h-3.5 w-3.5 accent-sky-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-4 py-3.5 align-top">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-zinc-100">{b.targetName}</span>
                        <Badge variant="neutral">{b.targetType}</Badge>
                      </div>
                      <div className="mt-0.5 text-xs text-zinc-500">
                        {b.trigger} · {b.volumeCount} mount{b.volumeCount === 1 ? '' : 's'}
                      </div>
                      <button
                        type="button"
                        onClick={() => openNote(b)}
                        title={b.note ? 'Edit note' : 'Add a note to this backup'}
                        className={`mt-1 flex max-w-xs items-start gap-1 text-left text-[11px] cursor-pointer ${
                          b.note ? 'text-amber-200/90 hover:text-amber-100' : 'text-zinc-600 hover:text-zinc-300'
                        }`}
                      >
                        <StickyNote className="mt-px h-3 w-3 shrink-0" />
                        <span className="break-words">{b.note || 'Add note'}</span>
                      </button>
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
                            {(b.compression === 'zstd' || b.fileName?.includes('.tar.zst')) && (
                              <Badge variant="neutral" className="text-[10px] py-0 px-1.5">
                                ZSTD
                              </Badge>
                            )}
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
                              onClick={() => openRestore(b)}
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
              <div className="flex items-center gap-1.5">
                <Button
                  size="sm"
                  disabled={inspectSelection.length === 0}
                  title="Restore only the checked files/folders"
                  onClick={() => openRestore(inspectItem, inspectSelection)}
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  Restore selected{inspectSelection.length ? ` (${inspectSelection.length})` : ''}
                </Button>
                <Button variant="outline" size="sm" onClick={handleVerify} disabled={verifyLoading}>
                  <ShieldCheck className="h-3.5 w-3.5" />
                  {verifyLoading ? 'Verifying...' : 'Verify SHA-256'}
                </Button>
              </div>
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
                        <input
                          type="checkbox"
                          aria-label={`Select ${entry.name} for restore`}
                          checked={inspectSelection.includes(entry.name)}
                          onChange={() => toggleInspectPath(entry.name)}
                          className="h-3 w-3 shrink-0 accent-sky-500 cursor-pointer"
                        />
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

            {inspectSteps.length > 0 && (
              <div className="space-y-2 pt-1">
                <div className="text-xs font-medium text-zinc-300">Job Step Timeline</div>
                <div className="max-h-48 overflow-y-auto pr-1">
                  <JobStepsList steps={inspectSteps} />
                </div>
              </div>
            )}
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
            {restorePaths.length > 0 && (
              <div className="rounded-xl border border-sky-500/30 bg-sky-500/10 p-3 text-xs text-sky-200">
                <div className="mb-1 font-medium">
                  Selective restore: only {restorePaths.length} selected path(s) will be extracted
                </div>
                <div className="max-h-24 space-y-0.5 overflow-y-auto font-mono text-[11px] text-sky-100/90">
                  {restorePaths.map((p) => (
                    <div key={p} className="truncate">
                      {p}
                    </div>
                  ))}
                </div>
              </div>
            )}
            <div className="grid grid-cols-2 gap-2 rounded-xl border border-zinc-800 bg-zinc-950/70 p-1">
              <button
                type="button"
                onClick={() => setRestoreMode('inplace')}
                className={`rounded-lg px-3 py-2 text-xs font-medium transition-colors cursor-pointer ${
                  restoreMode === 'inplace'
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                In-Place Restore
              </button>
              <button
                type="button"
                onClick={() => setRestoreMode('staging')}
                className={`rounded-lg px-3 py-2 text-xs font-medium transition-colors cursor-pointer ${
                  restoreMode === 'staging'
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                Staging Volume (Safe)
              </button>
            </div>

            {restoreMode === 'staging' ? (
              <div className="space-y-3">
                <div className="rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-3 text-xs text-emerald-200">
                  Creates/uses a separate staging Docker volume without overwriting or pausing any live container mounts.
                </div>
                <div>
                  <Label htmlFor="restore-staging-vol">Staging Docker Volume Name</Label>
                  <Input
                    id="restore-staging-vol"
                    value={restoreVolumeName}
                    onChange={(e) => setRestoreVolumeName(e.target.value)}
                    placeholder="my-app-restored"
                    className="font-mono"
                    required
                  />
                </div>
              </div>
            ) : (
              <>
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

                <div className="space-y-2">
                  <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                    <div className="pr-2">
                      <div className="text-xs font-medium text-zinc-200">
                        Take pre-restore safety snapshot
                      </div>
                      <p className="text-[11px] text-zinc-400">
                        Saves current live data before overwriting.
                      </p>
                    </div>
                    <Switch checked={restorePreSnap} onChange={setRestorePreSnap} />
                  </div>

                  <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                    <span className="text-xs text-zinc-200">
                      Pause running target container(s) during restore
                    </span>
                    <Switch checked={restorePause} onChange={setRestorePause} />
                  </div>
                </div>
              </>
            )}

            <div className="flex items-center justify-end gap-2 pt-2">
              <Button type="button" variant="ghost" onClick={() => setRestoreItem(null)}>
                Cancel
              </Button>
              <Button type="submit" disabled={restoring}>
                <RotateCcw className="h-4 w-4" />
                {restoring
                  ? 'Restoring...'
                  : restoreMode === 'staging'
                    ? 'Extract to Staging Volume'
                    : 'Confirm & Restore'}
              </Button>
            </div>
          </form>
        )}
      </Modal>

      {/* Backup Note Modal */}
      <Modal
        open={Boolean(noteItem)}
        onClose={() => setNoteItem(null)}
        title="Backup Note"
        subtitle={noteItem ? `${noteItem.targetName} · ${formatDateTime(noteItem.startedAt)}` : ''}
      >
        {noteItem && (
          <form onSubmit={handleSaveNote} className="space-y-3">
            <div>
              <Label htmlFor="backup-note-edit">Note</Label>
              <textarea
                id="backup-note-edit"
                rows={3}
                maxLength={500}
                value={noteDraft}
                onChange={(e) => setNoteDraft(e.target.value)}
                placeholder="e.g. last known-good state before upgrading to v2.0"
                style={{
                  backgroundColor: 'var(--dv-input-bg)',
                  borderColor: 'var(--dv-border-strong)',
                  color: 'var(--dv-text)',
                }}
                className="w-full rounded-lg border px-3 py-2 text-xs placeholder:text-zinc-500 focus:outline-none focus:ring-2 focus:ring-sky-500/40"
              />
              <div className="mt-1 text-right text-[11px] text-zinc-500">{noteDraft.length}/500</div>
            </div>
            <div className="flex items-center justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setNoteItem(null)}>
                Cancel
              </Button>
              <Button type="submit" disabled={savingNote}>
                {savingNote ? 'Saving...' : 'Save Note'}
              </Button>
            </div>
          </form>
        )}
      </Modal>

      {/* Snapshot Compare Modal */}
      <Modal
        open={Boolean(compareData)}
        onClose={() => setCompareData(null)}
        title="Compare Snapshots"
        subtitle={
          compareData?.base
            ? `${formatDateTime(compareData.base.startedAt)} → ${formatDateTime(compareData.target.startedAt)}`
            : ''
        }
        maxWidth="max-w-3xl"
      >
        {compareData?.loading ? (
          <div className="flex items-center justify-center py-10 text-sm text-zinc-400">
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            Reading both archives...
          </div>
        ) : compareData?.summary ? (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-2 text-xs">
              {[compareData.base, compareData.target].map((side, idx) => (
                <div key={side.id} className="rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
                  <div className="text-zinc-500">{idx === 0 ? 'Older (base)' : 'Newer'}</div>
                  <div className="mt-1 truncate font-mono text-zinc-200">{side.fileName}</div>
                  <div className="mt-0.5 text-zinc-400">
                    {side.fileCount} files · {formatBytes(side.totalBytes)}
                  </div>
                  {side.note && <div className="mt-1 text-amber-200/90">{side.note}</div>}
                </div>
              ))}
            </div>

            <div className="flex flex-wrap gap-2">
              <Badge variant="success">+{compareData.summary.added} added ({formatBytes(compareData.summary.addedBytes)})</Badge>
              <Badge variant="danger">−{compareData.summary.removed} removed ({formatBytes(compareData.summary.removedBytes)})</Badge>
              <Badge variant="warning">~{compareData.summary.changed} changed</Badge>
              <Badge variant="neutral">{compareData.summary.unchanged} unchanged</Badge>
            </div>
            {compareData.truncated && (
              <p className="text-[11px] text-zinc-500">Large diff: each list shows the first 1000 entries.</p>
            )}

            <div className="max-h-[45vh] overflow-y-auto rounded-xl border border-zinc-800 bg-zinc-950 p-2 font-mono text-xs space-y-0.5">
              {compareData.added.length + compareData.removed.length + compareData.changed.length === 0 ? (
                <div className="py-6 text-center text-zinc-500">No file differences — the snapshots are identical.</div>
              ) : (
                <>
                  {compareData.added.map((f) => (
                    <div key={`a-${f.name}`} className="flex justify-between gap-3 rounded px-2 py-0.5 text-emerald-300">
                      <span className="truncate">+ {f.name}</span>
                      <span className="shrink-0 text-[11px] text-zinc-500">{formatBytes(f.size)}</span>
                    </div>
                  ))}
                  {compareData.removed.map((f) => (
                    <div key={`r-${f.name}`} className="flex justify-between gap-3 rounded px-2 py-0.5 text-rose-300">
                      <span className="truncate">− {f.name}</span>
                      <span className="shrink-0 text-[11px] text-zinc-500">{formatBytes(f.size)}</span>
                    </div>
                  ))}
                  {compareData.changed.map((f) => (
                    <div key={`c-${f.name}`} className="flex justify-between gap-3 rounded px-2 py-0.5 text-amber-300">
                      <span className="truncate">~ {f.name}</span>
                      <span className="shrink-0 text-[11px] text-zinc-500">
                        {formatBytes(f.oldSize)} → {formatBytes(f.size)}
                      </span>
                    </div>
                  ))}
                </>
              )}
            </div>
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
