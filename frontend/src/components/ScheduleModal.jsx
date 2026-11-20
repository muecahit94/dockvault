import { useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  CalendarClock,
  Check,
  Container,
  HardDrive,
  Layers,
  Search,
  X,
} from 'lucide-react'
import { api, formatDateTime } from '../api/client'
import { Badge, Button, Input, Label, Modal, Select, Switch } from './ui'

const CRON_PRESETS = [
  { label: 'Daily 02:00', expr: '0 2 * * *' },
  { label: 'Every 6h', expr: '0 */6 * * *' },
  { label: 'Hourly', expr: '0 * * * *' },
  { label: 'Weekly Sun', expr: '0 3 * * 0' },
]

const targetKey = (t) => `${t.targetType}:${t.targetId}`

function targetIcon(type) {
  if (type === 'stack') return Layers
  if (type === 'volume') return HardDrive
  return Container
}

function mountKey(m) {
  return m.type === 'volume' && m.name ? m.name : m.source || m.name || m.destination
}

function mountLabel(m) {
  return m.type === 'volume' ? m.name || m.source : m.source
}

function mountMatchesList(m, selectedMounts) {
  if (!Array.isArray(selectedMounts)) return true
  return selectedMounts.some(
    (k) => k === m.source || (m.name && k === m.name) || k === m.destination,
  )
}

export function ScheduleModal({
  open,
  onClose,
  initialTarget,
  existingSchedule,
  providers,
  containers,
  stacks,
  volumes = [],
  defaultPauseBackups = true,
  onSaved,
  notify,
}) {
  const defaultProvider = providers.find((p) => p.isDefault) || providers[0]

  const [name, setName] = useState('')
  const [selectedTargets, setSelectedTargets] = useState([])
  const [targetFilterType, setTargetFilterType] = useState('all')
  const [targetQuery, setTargetQuery] = useState('')
  const [cronExpr, setCronExpr] = useState('0 2 * * *')
  const [storageProviderId, setStorageProviderId] = useState('')
  const [secondaryStorageId, setSecondaryStorageId] = useState('')
  const [retentionPolicy, setRetentionPolicy] = useState('count')
  const [retentionCount, setRetentionCount] = useState(7)
  const [keepDaily, setKeepDaily] = useState(7)
  const [keepWeekly, setKeepWeekly] = useState(4)
  const [keepMonthly, setKeepMonthly] = useState(3)
  const [preCommand, setPreCommand] = useState('')
  const [postCommand, setPostCommand] = useState('')
  const [compression, setCompression] = useState('gzip')
  const [excludePatterns, setExcludePatterns] = useState('')
  const [dbHotDump, setDbHotDump] = useState(false)
  const [pauseContainer, setPauseContainer] = useState(true)
  const [enabled, setEnabled] = useState(true)
  const [alertAfterHours, setAlertAfterHours] = useState(0)
  const [preview, setPreview] = useState({ valid: true, nextRunAt: null, error: '' })
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setTargetFilterType('all')
    setTargetQuery('')
    if (existingSchedule) {
      const initialList = existingSchedule.targets?.length
        ? existingSchedule.targets.map((t) => ({
            targetType: t.targetType,
            targetId: t.targetId,
            targetName: t.targetName || t.targetId,
            selectedMounts: t.selectedMounts?.length ? t.selectedMounts : null,
          }))
        : existingSchedule.targetId
          ? [
              {
                targetType: existingSchedule.targetType,
                targetId: existingSchedule.targetId,
                targetName: existingSchedule.targetName || existingSchedule.targetId,
                selectedMounts: existingSchedule.selectedMounts?.length
                  ? existingSchedule.selectedMounts
                  : null,
              },
            ]
          : []
      setName(existingSchedule.name)
      setSelectedTargets(initialList)
      setCronExpr(existingSchedule.cronExpr)
      setStorageProviderId(String(existingSchedule.storageProviderId))
      setSecondaryStorageId(
        existingSchedule.secondaryStorageId ? String(existingSchedule.secondaryStorageId) : '',
      )
      setRetentionPolicy(existingSchedule.retentionPolicy || 'count')
      setRetentionCount(existingSchedule.retentionCount ?? 7)
      setKeepDaily(existingSchedule.keepDaily ?? 7)
      setKeepWeekly(existingSchedule.keepWeekly ?? 4)
      setKeepMonthly(existingSchedule.keepMonthly ?? 3)
      setPreCommand(existingSchedule.preCommand || '')
      setPostCommand(existingSchedule.postCommand || '')
      setCompression(existingSchedule.compression || 'gzip')
      setExcludePatterns(existingSchedule.excludePatterns || '')
      setDbHotDump(Boolean(existingSchedule.dbHotDump))
      setPauseContainer(existingSchedule.pauseContainer ?? true)
      setEnabled(existingSchedule.enabled ?? true)
      setAlertAfterHours(existingSchedule.alertAfterHours ?? 0)
    } else if (initialTarget) {
      setName(`${initialTarget.targetName} Backup`)
      setSelectedTargets([
        {
          targetType: initialTarget.targetType,
          targetId: initialTarget.targetId,
          targetName: initialTarget.targetName || initialTarget.targetId,
          selectedMounts: null,
        },
      ])
      setCronExpr(initialTarget.gitopsSchedule || '0 2 * * *')
      setStorageProviderId(String(defaultProvider?.id || ''))
      setSecondaryStorageId('')
      setRetentionPolicy('count')
      setRetentionCount(7)
      setKeepDaily(7)
      setKeepWeekly(4)
      setKeepMonthly(3)
      setPreCommand('')
      setPostCommand('')
      setCompression(initialTarget.gitopsCompression || 'gzip')
      setExcludePatterns(initialTarget.gitopsExcludePaths || '')
      setDbHotDump(
        Boolean(initialTarget.detectedDb || initialTarget.detectedDbs?.length),
      )
      setPauseContainer(defaultPauseBackups)
      setEnabled(true)
      setAlertAfterHours(0)
    } else {
      const firstStack = stacks[0]
      const firstContainer = containers.find((c) => c.backupableCount > 0) || containers[0]
      const firstVol = volumes[0]
      const initialList = firstStack
        ? [{ targetType: 'stack', targetId: firstStack.name, targetName: firstStack.name, selectedMounts: null }]
        : firstContainer
          ? [{ targetType: 'container', targetId: firstContainer.name, targetName: firstContainer.name, selectedMounts: null }]
          : firstVol
            ? [{ targetType: 'volume', targetId: firstVol.name, targetName: firstVol.name, selectedMounts: null }]
            : []
      setName(initialList[0] ? `${initialList[0].targetName} Backup` : '')
      setSelectedTargets(initialList)
      setCronExpr('0 2 * * *')
      setStorageProviderId(String(defaultProvider?.id || ''))
      setSecondaryStorageId('')
      setRetentionPolicy('count')
      setRetentionCount(7)
      setKeepDaily(7)
      setKeepWeekly(4)
      setKeepMonthly(3)
      setPreCommand('')
      setPostCommand('')
      setCompression('gzip')
      setExcludePatterns('')
      setDbHotDump(false)
      setPauseContainer(defaultPauseBackups)
      setEnabled(true)
      setAlertAfterHours(0)
    }
    // Intentionally initialize only when modal opens or target/schedule changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, existingSchedule, initialTarget])

  useEffect(() => {
    if (!open || !cronExpr.trim()) return
    let active = true
    const timer = setTimeout(async () => {
      try {
        const res = await api.previewCron(cronExpr)
        if (active) setPreview({ valid: true, nextRunAt: res.nextRunAt, error: '' })
      } catch (err) {
        if (active) setPreview({ valid: false, nextRunAt: null, error: err.message })
      }
    }, 200)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [open, cronExpr])

  const selectedKeySet = useMemo(
    () => new Set(selectedTargets.map(targetKey)),
    [selectedTargets],
  )

  const selectedStackNames = useMemo(
    () =>
      new Set(
        selectedTargets
          .filter((t) => t.targetType === 'stack')
          .map((t) => t.targetId),
      ),
    [selectedTargets],
  )

  const containerProjectByKey = useMemo(() => {
    const map = new Map()
    for (const c of containers) {
      if (!c.composeProject) continue
      map.set(`container:${c.name}`, c.composeProject)
      if (c.id) map.set(`container:${c.id}`, c.composeProject)
    }
    return map
  }, [containers])

  const allAvailableTargets = useMemo(() => {
    const stackItems = stacks.map((s) => ({
      targetType: 'stack',
      targetId: s.name,
      targetName: s.name,
      composeProject: '',
      meta: `${s.runningCount}/${s.totalCount} running · ${s.backupableCount} mounts`,
    }))
    const containerItems = containers
      .filter(
        (c) =>
          c.backupableCount > 0 ||
          selectedKeySet.has(`container:${c.name}`) ||
          selectedKeySet.has(`container:${c.id}`),
      )
      .map((c) => ({
        targetType: 'container',
        targetId: c.name,
        targetName: c.name,
        composeProject: c.composeProject || '',
        meta: `${c.state} · ${c.backupableCount} mounts${c.composeProject ? ` · ${c.composeProject}` : ''}`,
      }))
    const volumeItems = volumes.map((v) => ({
      targetType: 'volume',
      targetId: v.name,
      targetName: v.name,
      composeProject: '',
      meta: v.inUse ? 'attached' : 'orphan',
    }))
    return [...stackItems, ...containerItems, ...volumeItems]
  }, [stacks, containers, volumes, selectedKeySet])

  const filteredTargets = useMemo(() => {
    const q = targetQuery.trim().toLowerCase()
    return allAvailableTargets.filter((t) => {
      if (targetFilterType !== 'all' && t.targetType !== targetFilterType) return false
      if (!q) return true
      return (
        t.targetName.toLowerCase().includes(q) ||
        t.targetType.toLowerCase().includes(q) ||
        t.meta.toLowerCase().includes(q)
      )
    })
  }, [allAvailableTargets, targetFilterType, targetQuery])

  const mountsByTargetKey = useMemo(() => {
    const map = new Map()
    for (const s of stacks) {
      map.set(`stack:${s.name}`, s.uniqueMounts || [])
    }
    for (const c of containers) {
      const mList = (c.mounts || []).filter((m) => m.backupable)
      map.set(`container:${c.name}`, mList)
      if (c.id) map.set(`container:${c.id}`, mList)
    }
    return map
  }, [stacks, containers])

  function toggleTarget(item) {
    const key = targetKey(item)
    setSelectedTargets((prev) => {
      if (prev.some((p) => targetKey(p) === key)) {
        return prev.filter((p) => targetKey(p) !== key)
      }
      if (
        item.targetType === 'container' &&
        item.composeProject &&
        prev.some((p) => p.targetType === 'stack' && p.targetId === item.composeProject)
      ) {
        return prev
      }
      const next =
        item.targetType === 'stack'
          ? prev.filter(
              (p) =>
                p.targetType !== 'container' ||
                containerProjectByKey.get(targetKey(p)) !== item.targetId,
            )
          : prev
      return [
        ...next,
        {
          targetType: item.targetType,
          targetId: item.targetId,
          targetName: item.targetName,
          selectedMounts: null,
        },
      ]
    })
  }

  function selectAllStacks() {
    setSelectedTargets((prev) => {
      const allStackIds = new Set(stacks.map((s) => s.name))
      const next = prev.filter(
        (p) =>
          p.targetType !== 'container' ||
          !allStackIds.has(containerProjectByKey.get(targetKey(p))),
      )
      const seen = new Set(next.map(targetKey))
      for (const s of stacks) {
        const t = { targetType: 'stack', targetId: s.name, targetName: s.name, selectedMounts: null }
        const key = targetKey(t)
        if (!seen.has(key)) {
          seen.add(key)
          next.push(t)
        }
      }
      return next
    })
  }

  function toggleMountForTarget(targetItem, mount, allMounts) {
    const tKey = targetKey(targetItem)
    const clickedKey = mountKey(mount)
    setSelectedTargets((prev) =>
      prev.map((t) => {
        if (targetKey(t) !== tKey) return t
        const currentKeys = allMounts
          .filter((m) => mountMatchesList(m, t.selectedMounts))
          .map(mountKey)
        const nextKeys = currentKeys.includes(clickedKey)
          ? currentKeys.filter((k) => k !== clickedKey)
          : [...currentKeys, clickedKey]
        const nextAll =
          nextKeys.length >= allMounts.length &&
          allMounts.every((m) => nextKeys.includes(mountKey(m)))
        return {
          ...t,
          selectedMounts: nextAll ? null : nextKeys,
        }
      }),
    )
  }

  function setAllMountsForTarget(targetItem, selectAll) {
    const tKey = targetKey(targetItem)
    setSelectedTargets((prev) =>
      prev.map((t) =>
        targetKey(t) === tKey ? { ...t, selectedMounts: selectAll ? null : [] } : t,
      ),
    )
  }

  const activePrimaryId = storageProviderId || String(defaultProvider?.id || '')
  const hasContainerOrStack =
    selectedTargets.length === 0 || selectedTargets.some((t) => t.targetType !== 'volume')

  const targetsWithMounts = selectedTargets
    .filter((t) => t.targetType !== 'volume')
    .map((t) => ({
      target: t,
      mounts: mountsByTargetKey.get(targetKey(t)) || [],
    }))
    .filter((entry) => entry.mounts.length > 0)

  async function handleSubmit(e) {
    e.preventDefault()
    if (selectedTargets.length === 0) {
      notify('error', 'Select at least one stack, container, or volume')
      return
    }
    for (const entry of targetsWithMounts) {
      if (Array.isArray(entry.target.selectedMounts) && entry.target.selectedMounts.length === 0) {
        notify('error', `Select at least one volume or mount for "${entry.target.targetName}"`)
        return
      }
    }
    setSaving(true)
    try {
      const normalizedTargets = selectedTargets.map((t) => ({
        targetType: t.targetType,
        targetId: t.targetId,
        targetName: t.targetName,
        ...(Array.isArray(t.selectedMounts) && t.selectedMounts.length > 0
          ? { selectedMounts: t.selectedMounts }
          : {}),
      }))
      const defaultName =
        normalizedTargets.length === 1
          ? `${normalizedTargets[0].targetName} Backup`
          : `${normalizedTargets.length} Targets Backup`
      const payload = {
        name: name.trim() || defaultName,
        targetType: normalizedTargets[0].targetType,
        targetId: normalizedTargets[0].targetId,
        targetName: normalizedTargets[0].targetName,
        targets: normalizedTargets,
        cronExpr: cronExpr.trim(),
        storageProviderId: Number(activePrimaryId) || 0,
        secondaryStorageId: secondaryStorageId ? Number(secondaryStorageId) : 0,
        retentionPolicy,
        retentionCount: Number(retentionCount) || 0,
        keepDaily: Number(keepDaily) || 0,
        keepWeekly: Number(keepWeekly) || 0,
        keepMonthly: Number(keepMonthly) || 0,
        preCommand: preCommand.trim(),
        postCommand: postCommand.trim(),
        compression,
        excludePatterns: excludePatterns.trim(),
        dbHotDump,
        pauseContainer,
        enabled,
        alertAfterHours: Math.max(0, Number(alertAfterHours) || 0),
      }
      if (existingSchedule) {
        await api.updateSchedule(existingSchedule.id, payload)
        notify('success', 'Schedule updated')
      } else {
        await api.createSchedule(payload)
        notify('success', 'Schedule created')
      }
      onSaved?.()
      onClose()
    } catch (err) {
      notify('error', err.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={existingSchedule ? 'Edit Backup Schedule' : 'Create Backup Schedule'}
      subtitle="Automate recurring backups with 3-2-1 offsite copies and GFS retention"
      maxWidth="max-w-xl"
    >
      <form onSubmit={handleSubmit} className="space-y-4 max-h-[80vh] overflow-y-auto pr-1">
        <div>
          <Label htmlFor="sched-name">Schedule Name</Label>
          <Input
            id="sched-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Nightly Postgres Backup"
            required
          />
        </div>

        <div className="space-y-2 rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Label className="mb-0">
              Activated Stacks &amp; Targets ({selectedTargets.length} selected)
            </Label>
            <div className="flex items-center gap-1.5">
              {stacks.length > 0 && (
                <button
                  type="button"
                  onClick={selectAllStacks}
                  className="rounded-md border border-zinc-700/80 bg-zinc-800/80 px-2 py-0.5 text-[11px] text-zinc-300 hover:border-sky-500/50 hover:text-sky-300 cursor-pointer"
                >
                  Activate All Stacks ({stacks.length})
                </button>
              )}
              {selectedTargets.length > 0 && (
                <button
                  type="button"
                  onClick={() => setSelectedTargets([])}
                  className="rounded-md border border-zinc-800 bg-zinc-900 px-2 py-0.5 text-[11px] text-zinc-400 hover:text-zinc-200 cursor-pointer"
                >
                  Clear
                </button>
              )}
            </div>
          </div>

          {selectedTargets.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {selectedTargets.map((t) => {
                const Icon = targetIcon(t.targetType)
                return (
                  <button
                    key={targetKey(t)}
                    type="button"
                    onClick={() => toggleTarget(t)}
                    title="Click to remove from schedule"
                    className="inline-flex cursor-pointer items-center gap-1 rounded-md border border-sky-500/30 bg-sky-500/10 px-2 py-0.5 text-[11px] text-sky-300 hover:bg-sky-500/20"
                  >
                    <Icon className="h-3 w-3 shrink-0" />
                    <span>{t.targetName}</span>
                    <X className="h-2.5 w-2.5 opacity-80" />
                  </button>
                )
              })}
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <div className="relative flex-1 min-w-[180px]">
              <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
              <Input
                value={targetQuery}
                onChange={(e) => setTargetQuery(e.target.value)}
                placeholder="Filter stacks, containers, volumes..."
                className="pl-8 h-8 text-xs"
              />
            </div>
            <div className="flex items-center rounded-lg border border-zinc-800 bg-zinc-900 p-0.5 text-[11px]">
              {[
                { id: 'all', label: 'All' },
                { id: 'stack', label: 'Stacks' },
                { id: 'container', label: 'Containers' },
                { id: 'volume', label: 'Volumes' },
              ].map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setTargetFilterType(tab.id)}
                  className={`rounded px-2 py-0.5 font-medium transition-colors cursor-pointer ${
                    targetFilterType === tab.id
                      ? 'bg-zinc-800 text-zinc-100'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>
          </div>

          <div className="max-h-44 space-y-1 overflow-y-auto pr-1">
            {filteredTargets.length === 0 ? (
              <div className="py-3 text-center text-xs text-zinc-500">
                No matching stacks, containers, or volumes found.
              </div>
            ) : (
              filteredTargets.map((item) => {
                const key = targetKey(item)
                const coveredByStack =
                  item.targetType === 'container' &&
                  Boolean(item.composeProject) &&
                  selectedStackNames.has(item.composeProject)
                const checked = selectedKeySet.has(key) || coveredByStack
                const Icon = targetIcon(item.targetType)
                return (
                  <label
                    key={key}
                    title={
                      coveredByStack
                        ? `Already included via stack "${item.composeProject}". Unselect the stack to pick individual containers.`
                        : undefined
                    }
                    className={`flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-xs transition-colors ${
                      coveredByStack
                        ? 'bg-sky-500/5 text-zinc-400 opacity-75 cursor-not-allowed'
                        : checked
                          ? 'bg-sky-500/10 text-zinc-100 cursor-pointer'
                          : 'hover:bg-zinc-900 text-zinc-300 cursor-pointer'
                    }`}
                  >
                    <span className="flex items-center gap-2 min-w-0">
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={coveredByStack}
                        onChange={() => !coveredByStack && toggleTarget(item)}
                        className="h-3.5 w-3.5 accent-sky-500 cursor-pointer disabled:cursor-not-allowed shrink-0"
                      />
                      <Icon
                        className={`h-3.5 w-3.5 shrink-0 ${
                          item.targetType === 'stack'
                            ? 'text-violet-400'
                            : item.targetType === 'volume'
                              ? 'text-amber-400'
                              : 'text-sky-400'
                        }`}
                      />
                      <span className="truncate font-medium">{item.targetName}</span>
                      <Badge variant="neutral" className="text-[10px] py-0 px-1.5">
                        {item.targetType}
                      </Badge>
                      {coveredByStack && (
                        <Badge variant="info" className="text-[10px] py-0 px-1.5">
                          via {item.composeProject}
                        </Badge>
                      )}
                    </span>
                    <span className="shrink-0 font-mono text-[10px] text-zinc-500">
                      {item.meta}
                    </span>
                  </label>
                )
              })
            )}
          </div>
        </div>

        {targetsWithMounts.length > 0 && (
          <div className="space-y-2.5 rounded-xl border border-zinc-800 bg-zinc-950/60 p-3">
            <div className="flex items-center justify-between gap-2">
              <Label className="mb-0 flex items-center gap-1.5">
                <HardDrive className="h-3.5 w-3.5 text-sky-400" />
                Included Volumes &amp; Mounts
              </Label>
              <span className="text-[11px] text-zinc-400">
                Leave all checked to automatically include new volumes too
              </span>
            </div>

            <div className="max-h-52 space-y-2.5 overflow-y-auto pr-1">
              {targetsWithMounts.map(({ target, mounts }) => {
                const Icon = targetIcon(target.targetType)
                const checkedCount = mounts.filter((m) =>
                  mountMatchesList(m, target.selectedMounts),
                ).length
                const allIncluded = !Array.isArray(target.selectedMounts)
                return (
                  <div
                    key={targetKey(target)}
                    className="rounded-lg border border-zinc-800/80 bg-zinc-900/50 p-2.5 space-y-1.5"
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                      <span className="flex items-center gap-1.5 font-medium text-zinc-200">
                        <Icon
                          className={`h-3.5 w-3.5 ${
                            target.targetType === 'stack' ? 'text-violet-400' : 'text-sky-400'
                          }`}
                        />
                        <span>{target.targetName}</span>
                        <Badge variant={allIncluded ? 'neutral' : 'info'} className="text-[10px]">
                          {allIncluded
                            ? `All ${mounts.length} mounts`
                            : `${checkedCount}/${mounts.length} selected`}
                        </Badge>
                      </span>
                      {mounts.length > 1 && (
                        <button
                          type="button"
                          onClick={() => setAllMountsForTarget(target, checkedCount < mounts.length)}
                          className="text-[11px] text-sky-400 hover:text-sky-300 cursor-pointer"
                        >
                          {checkedCount === mounts.length ? 'Clear all' : 'Select all'}
                        </button>
                      )}
                    </div>

                    <div className="space-y-1">
                      {mounts.map((m, idx) => {
                        const key = mountKey(m)
                        const checked = mountMatchesList(m, target.selectedMounts)
                        return (
                          <label
                            key={`${targetKey(target)}-${key}-${idx}`}
                            className="flex items-center justify-between gap-2 rounded-md border border-zinc-800/70 bg-zinc-950/60 px-2 py-1 text-xs cursor-pointer hover:border-zinc-700"
                          >
                            <span className="flex items-center gap-2 min-w-0">
                              <input
                                type="checkbox"
                                checked={checked}
                                onChange={() => toggleMountForTarget(target, m, mounts)}
                                className="h-3.5 w-3.5 accent-sky-500 cursor-pointer shrink-0"
                              />
                              <span className="truncate font-mono text-zinc-200">
                                {mountLabel(m)}
                              </span>
                            </span>
                            <span className="ml-2 shrink-0 text-[11px] text-zinc-400">
                              → {m.destination}
                            </span>
                          </label>
                        )
                      })}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <Label htmlFor="sched-cron" className="mb-0">
              Cron Expression
            </Label>
            <div className="flex flex-wrap gap-1">
              {CRON_PRESETS.map((preset) => (
                <button
                  key={preset.expr}
                  type="button"
                  onClick={() => setCronExpr(preset.expr)}
                  className="rounded-md border border-zinc-700/80 bg-zinc-800/80 px-2 py-0.5 text-[11px] text-zinc-300 hover:border-sky-500/50 hover:text-sky-300 cursor-pointer"
                >
                  {preset.label}
                </button>
              ))}
            </div>
          </div>
          <Input
            id="sched-cron"
            value={cronExpr}
            onChange={(e) => setCronExpr(e.target.value)}
            className="font-mono"
            placeholder="0 2 * * *"
            required
          />
          <div className="mt-1.5 flex items-center gap-1.5 text-xs">
            {preview.valid ? (
              <>
                <Check className="h-3.5 w-3.5 text-emerald-400" />
                <span className="text-zinc-400">
                  Next run: <span className="text-zinc-200">{formatDateTime(preview.nextRunAt)}</span>
                </span>
              </>
            ) : (
              <>
                <AlertTriangle className="h-3.5 w-3.5 text-rose-400" />
                <span className="text-rose-300">{preview.error}</span>
              </>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <Label htmlFor="sched-storage">Primary Storage</Label>
            <Select
              id="sched-storage"
              value={activePrimaryId}
              onChange={(e) => setStorageProviderId(e.target.value)}
            >
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.type.toUpperCase()}){p.encryptBackups ? ' 🔒' : ''}
                </option>
              ))}
            </Select>
          </div>
          <div>
            <Label htmlFor="sched-sec-storage">Secondary Offsite (3-2-1)</Label>
            <Select
              id="sched-sec-storage"
              value={secondaryStorageId}
              onChange={(e) => setSecondaryStorageId(e.target.value)}
            >
              <option value="">None (single destination)</option>
              {providers
                .filter((p) => String(p.id) !== String(activePrimaryId))
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name} ({p.type.toUpperCase()}){p.encryptBackups ? ' 🔒' : ''}
                  </option>
                ))}
            </Select>
          </div>
        </div>

        {/* Retention Policy */}
        <div className="rounded-xl border border-zinc-800 bg-zinc-950/50 p-3 space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 items-end">
            <div>
              <Label htmlFor="sched-ret-policy">Retention Policy</Label>
              <Select
                id="sched-ret-policy"
                value={retentionPolicy}
                onChange={(e) => setRetentionPolicy(e.target.value)}
              >
                <option value="count">Keep Latest N Backups</option>
                <option value="gfs">GFS (Daily / Weekly / Monthly)</option>
              </Select>
            </div>
            {retentionPolicy === 'count' && (
              <div>
                <Label htmlFor="sched-retention">Keep Count (0 = unlimited)</Label>
                <Input
                  id="sched-retention"
                  type="number"
                  min="0"
                  max="365"
                  value={retentionCount}
                  onChange={(e) => setRetentionCount(e.target.value)}
                />
              </div>
            )}
          </div>

          {retentionPolicy === 'gfs' && (
            <div className="grid grid-cols-3 gap-2">
              <div>
                <Label htmlFor="gfs-daily">Daily</Label>
                <Input
                  id="gfs-daily"
                  type="number"
                  min="0"
                  max="90"
                  value={keepDaily}
                  onChange={(e) => setKeepDaily(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="gfs-weekly">Weekly</Label>
                <Input
                  id="gfs-weekly"
                  type="number"
                  min="0"
                  max="52"
                  value={keepWeekly}
                  onChange={(e) => setKeepWeekly(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="gfs-monthly">Monthly</Label>
                <Input
                  id="gfs-monthly"
                  type="number"
                  min="0"
                  max="60"
                  value={keepMonthly}
                  onChange={(e) => setKeepMonthly(e.target.value)}
                />
              </div>
            </div>
          )}
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <Label htmlFor="sched-compression">Archive Compression</Label>
            <Select
              id="sched-compression"
              value={compression}
              onChange={(e) => setCompression(e.target.value)}
            >
              <option value="gzip">gzip (.tar.gz — Universal)</option>
              <option value="zstd">zstd (.tar.zst — Faster &amp; Smaller)</option>
            </Select>
          </div>
          <div>
            <Label htmlFor="sched-excludes">Exclude Patterns (optional)</Label>
            <Input
              id="sched-excludes"
              value={excludePatterns}
              onChange={(e) => setExcludePatterns(e.target.value)}
              placeholder="*.log, **/cache/**, node_modules"
              className="font-mono text-xs"
            />
          </div>
        </div>

        {hasContainerOrStack && (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <Label htmlFor="sched-pre">Pre-backup Exec Hook (optional)</Label>
              <Input
                id="sched-pre"
                value={preCommand}
                onChange={(e) => setPreCommand(e.target.value)}
                placeholder="pg_dumpall -U postgres > /data/dump.sql"
                className="font-mono text-xs"
              />
            </div>
            <div>
              <Label htmlFor="sched-post">Post-backup Cleanup (optional)</Label>
              <Input
                id="sched-post"
                value={postCommand}
                onChange={(e) => setPostCommand(e.target.value)}
                placeholder="rm -f /data/dump.sql"
                className="font-mono text-xs"
              />
            </div>
          </div>
        )}

        <div className="space-y-2">
          {hasContainerOrStack && (
            <>
              <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                <span className="text-xs text-zinc-200">
                  Zero-downtime database hot dumps (auto-detect postgres/mysql/mariadb/mongo/redis)
                </span>
                <Switch checked={dbHotDump} onChange={setDbHotDump} />
              </div>
              <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
                <span className="text-xs text-zinc-200">Pause running container(s) during backup</span>
                <Switch checked={pauseContainer} onChange={setPauseContainer} />
              </div>
            </>
          )}
          <div className="flex items-center justify-between gap-3 rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
            <div className="pr-2">
              <label htmlFor="sched-alert-after" className="text-xs text-zinc-200">
                Alert if no successful backup within (hours)
              </label>
              <p className="text-[11px] text-zinc-400">
                Sends an "overdue" alert to failure channels when backups silently stop. 0 = off; use 26 for a daily job.
              </p>
            </div>
            <Input
              id="sched-alert-after"
              type="number"
              min={0}
              max={8760}
              value={alertAfterHours}
              onChange={(e) => setAlertAfterHours(e.target.value)}
              className="w-24 font-mono text-xs"
            />
          </div>
          <div className="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950/50 px-3.5 py-2.5">
            <span className="text-xs text-zinc-200">Schedule enabled</span>
            <Switch checked={enabled} onChange={setEnabled} />
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={saving || !preview.valid}>
            <CalendarClock className="h-4 w-4" />
            {saving ? 'Saving...' : existingSchedule ? 'Update Schedule' : 'Create Schedule'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
