import { useEffect, useState } from 'react'
import { Archive, HardDrive, PauseCircle, Play, Terminal } from 'lucide-react'
import { api } from '../api/client'
import { Badge, Button, Input, Label, Modal, Select, Switch } from './ui'

function mountKey(m) {
  return m.source || m.name || m.destination
}

export function BackupModal({
  open,
  onClose,
  target,
  providers,
  defaultPauseBackups = true,
  onStarted,
  notify,
}) {
  const defaultProvider = providers.find((p) => p.isDefault) || providers[0]
  const [storageProviderId, setStorageProviderId] = useState('')
  const [secondaryStorageId, setSecondaryStorageId] = useState('')
  const [pauseContainer, setPauseContainer] = useState(defaultPauseBackups)
  const [selectedMounts, setSelectedMounts] = useState([])
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [preCommand, setPreCommand] = useState('')
  const [postCommand, setPostCommand] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open && target) {
      setStorageProviderId(defaultProvider?.id ? String(defaultProvider.id) : '')
      setSecondaryStorageId('')
      setPauseContainer(defaultPauseBackups)
      setSelectedMounts((target.mounts || []).map(mountKey))
      setShowAdvanced(false)
      setPreCommand('')
      setPostCommand('')
    }
  }, [open, target, defaultProvider?.id, defaultPauseBackups])

  if (!target) return null

  const activeProviderId = storageProviderId || defaultProvider?.id || ''
  const mounts = target.mounts || []

  function toggleMount(key) {
    setSelectedMounts((prev) =>
      prev.includes(key) ? prev.filter((item) => item !== key) : [...prev, key],
    )
  }

  async function handleSubmit(e) {
    e.preventDefault()
    if (selectedMounts.length === 0) {
      notify('error', 'Select at least one mount to back up')
      return
    }
    setSubmitting(true)
    try {
      const payload = {
        targetType: target.targetType,
        targetId: target.targetId,
        storageProviderId: Number(activeProviderId) || 0,
        secondaryStorageId: Number(secondaryStorageId) || null,
        pauseContainer,
        preCommand: preCommand.trim(),
        postCommand: postCommand.trim(),
      }
      if (selectedMounts.length < mounts.length) {
        payload.selectedMounts = selectedMounts
      }
      const res = await api.runBackup(payload)
      notify('success', `Backup started for ${target.targetName}`)
      onStarted?.(res.backup)
      onClose()
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Backup ${
        target.targetType === 'stack'
          ? 'Compose Stack'
          : target.targetType === 'volume'
            ? 'Docker Volume'
            : 'Container'
      }`}
      subtitle={`Target: ${target.targetName}`}
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="rounded-xl border border-zinc-800 bg-zinc-950/70 p-3">
          <div className="flex items-center justify-between text-xs font-medium text-zinc-300 mb-2">
            <span className="flex items-center gap-1.5">
              <HardDrive className="h-3.5 w-3.5 text-sky-400" />
              Included Mounts ({selectedMounts.length}/{mounts.length})
            </span>
            <div className="flex items-center gap-2">
              {mounts.length > 1 && (
                <button
                  type="button"
                  onClick={() =>
                    setSelectedMounts(
                      selectedMounts.length === mounts.length ? [] : mounts.map(mountKey),
                    )
                  }
                  className="text-[11px] text-sky-400 hover:text-sky-300 cursor-pointer"
                >
                  {selectedMounts.length === mounts.length ? 'Clear all' : 'Select all'}
                </button>
              )}
              <Badge variant="info">{target.targetType}</Badge>
            </div>
          </div>
          {mounts.length === 0 ? (
            <p className="text-xs text-amber-300">
              No backupable volumes or bind mounts detected on this target.
            </p>
          ) : (
            <div className="max-h-36 space-y-1.5 overflow-y-auto pr-1">
              {mounts.map((m, idx) => {
                const key = mountKey(m)
                const checked = selectedMounts.includes(key)
                return (
                  <label
                    key={`${m.type}-${key}-${idx}`}
                    className="flex items-center justify-between gap-2 rounded-lg border border-zinc-800/80 bg-zinc-900/60 px-2.5 py-1.5 text-xs cursor-pointer hover:border-zinc-700"
                  >
                    <span className="flex items-center gap-2 min-w-0">
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => toggleMount(key)}
                        className="accent-sky-500 rounded cursor-pointer"
                      />
                      <span className="truncate font-mono text-zinc-200">
                        {m.type === 'volume' ? m.name || m.source : m.source}
                      </span>
                    </span>
                    <span className="ml-2 shrink-0 text-[11px] text-zinc-400">
                      → {m.destination}
                    </span>
                  </label>
                )
              })}
            </div>
          )}
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <Label htmlFor="backup-storage">Primary Storage</Label>
            <Select
              id="backup-storage"
              value={activeProviderId}
              onChange={(e) => setStorageProviderId(e.target.value)}
            >
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.type.toUpperCase()}){p.encryptBackups ? ' 🔒' : ''}
                  {p.isDefault ? ' — Default' : ''}
                </option>
              ))}
            </Select>
          </div>
          <div>
            <Label htmlFor="backup-sec-storage">Secondary Offsite (3-2-1)</Label>
            <Select
              id="backup-sec-storage"
              value={secondaryStorageId}
              onChange={(e) => setSecondaryStorageId(e.target.value)}
            >
              <option value="">None (single copy)</option>
              {providers
                .filter((p) => String(p.id) !== String(activeProviderId))
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name} ({p.type.toUpperCase()}){p.encryptBackups ? ' 🔒' : ''}
                  </option>
                ))}
            </Select>
          </div>
        </div>

        {target.targetType !== 'volume' && (
          <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
            <div className="pr-3">
              <div className="flex items-center gap-1.5 text-xs font-medium text-zinc-200">
                <PauseCircle className="h-3.5 w-3.5 text-sky-400" />
                Pause container(s) during archive
              </div>
              <p className="mt-0.5 text-[11px] text-zinc-400">
                Freezes I/O via Docker pause for consistent database snapshots, then unpauses immediately.
              </p>
            </div>
            <Switch checked={pauseContainer} onChange={setPauseContainer} />
          </div>
        )}

        {target.targetType !== 'volume' && (
          <div>
            <button
              type="button"
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="flex items-center gap-1.5 text-xs text-zinc-400 hover:text-zinc-200 cursor-pointer"
            >
              <Terminal className="h-3.5 w-3.5" />
              {showAdvanced ? 'Hide pre/post exec hooks' : 'Add pre/post exec hooks (optional)'}
            </button>
            {showAdvanced && (
              <div className="mt-2 space-y-2 rounded-xl border border-zinc-800 bg-zinc-950/50 p-3">
                <div>
                  <Label htmlFor="bk-pre">Pre-backup exec command</Label>
                  <Input
                    id="bk-pre"
                    value={preCommand}
                    onChange={(e) => setPreCommand(e.target.value)}
                    placeholder="pg_dumpall -U postgres > /var/lib/postgresql/data/dump.sql"
                    className="font-mono text-xs"
                  />
                </div>
                <div>
                  <Label htmlFor="bk-post">Post-backup cleanup command</Label>
                  <Input
                    id="bk-post"
                    value={postCommand}
                    onChange={(e) => setPostCommand(e.target.value)}
                    placeholder="rm -f /var/lib/postgresql/data/dump.sql"
                    className="font-mono text-xs"
                  />
                </div>
              </div>
            )}
          </div>
        )}

        <div className="flex items-center justify-end gap-2 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={submitting || selectedMounts.length === 0}>
            {submitting ? (
              <>
                <Archive className="h-4 w-4 animate-pulse" />
                Starting...
              </>
            ) : (
              <>
                <Play className="h-4 w-4" />
                Run Backup Now
              </>
            )}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
