import { useEffect, useMemo, useState } from 'react'
import { Box, Container, HardDrive, Layers, Search } from 'lucide-react'
import { api } from '../api/client'
import { Badge, Button, Input, Modal } from './ui'

const GROUPS = [
  { type: 'stack', label: 'Compose Stacks', icon: Layers },
  { type: 'container', label: 'Containers', icon: Container },
  { type: 'volume', label: 'Volumes', icon: HardDrive },
]

const targetKey = (type, id) => `${type}:${id}`

export function ApplyScheduleModal({ open, onClose, source, schedules, stacks, containers, volumes, onApplied, notify }) {
  const [selected, setSelected] = useState([])
  const [query, setQuery] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // Reset only when the modal opens for a new source; polling refreshes must not clear the selection.
  useEffect(() => {
    if (open) {
      setSelected([])
      setQuery('')
    }
  }, [open, source?.id])

  const scheduledKeys = useMemo(
    () => new Set(schedules.map((s) => targetKey(s.targetType, s.targetId))),
    [schedules],
  )

  const groups = useMemo(() => {
    const q = query.trim().toLowerCase()
    const items = {
      stack: stacks.map((s) => ({ id: s.name, name: s.name })),
      container: containers
        .filter((c) => !c.isSelf && !c.excluded && c.backupableCount > 0)
        .map((c) => ({ id: c.name, name: c.name })),
      volume: volumes.map((v) => ({ id: v.name, name: v.name })),
    }
    return GROUPS.map((g) => ({
      ...g,
      items: items[g.type].filter((it) => !q || it.name.toLowerCase().includes(q)),
    }))
  }, [stacks, containers, volumes, query])

  if (!source) return null

  function toggle(key) {
    setSelected((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]))
  }

  async function handleApply() {
    if (selected.length === 0) return
    setSubmitting(true)
    try {
      const targets = selected.map((key) => {
        const idx = key.indexOf(':')
        const targetType = key.slice(0, idx)
        const targetId = key.slice(idx + 1)
        return { targetType, targetId, targetName: targetId }
      })
      const res = await api.cloneSchedule(source.id, targets)
      const createdCount = res.schedules?.length || 0
      const skipped = res.skipped || []
      notify(
        createdCount > 0 ? 'success' : 'info',
        `Created ${createdCount} schedule(s) from "${source.name}"${
          skipped.length ? ` · skipped already scheduled: ${skipped.join(', ')}` : ''
        }`,
      )
      onApplied?.()
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
      title="Apply Schedule to Other Targets"
      subtitle={`Copies cron, storage, retention, hooks & alert settings from "${source.name}"`}
      maxWidth="max-w-xl"
    >
      <div className="space-y-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-zinc-500" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter stacks, containers, volumes..."
            className="pl-9 h-9"
          />
        </div>

        <div className="max-h-[50vh] space-y-3 overflow-y-auto pr-1">
          {groups.map((group) => {
            const Icon = group.icon
            return (
              <div key={group.type}>
                <div className="mb-1 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-zinc-400">
                  <Icon className="h-3.5 w-3.5" />
                  {group.label}
                </div>
                {group.items.length === 0 ? (
                  <div className="px-2 py-1 text-xs text-zinc-500">None found</div>
                ) : (
                  <div className="space-y-0.5">
                    {group.items.map((it) => {
                      const key = targetKey(group.type, it.id)
                      const alreadyScheduled = scheduledKeys.has(key)
                      return (
                        <label
                          key={key}
                          className={`flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-xs ${
                            alreadyScheduled ? 'opacity-50' : 'cursor-pointer hover:bg-zinc-900'
                          }`}
                        >
                          <span className="flex items-center gap-2 truncate text-zinc-200">
                            <input
                              type="checkbox"
                              disabled={alreadyScheduled}
                              checked={selected.includes(key)}
                              onChange={() => toggle(key)}
                              className="h-3.5 w-3.5 accent-sky-500"
                            />
                            <Box className="h-3.5 w-3.5 shrink-0 text-zinc-500" />
                            <span className="truncate">{it.name}</span>
                          </span>
                          {alreadyScheduled && <Badge variant="neutral">Scheduled</Badge>}
                        </label>
                      )
                    })}
                  </div>
                )}
              </div>
            )
          })}
        </div>

        <div className="flex items-center justify-between gap-2 pt-2">
          <span className="text-xs text-zinc-400">{selected.length} selected</span>
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="button" onClick={handleApply} disabled={submitting || selected.length === 0}>
              {submitting ? 'Creating...' : `Create ${selected.length || ''} Schedule${selected.length === 1 ? '' : 's'}`}
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  )
}
