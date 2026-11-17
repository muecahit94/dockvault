import { useState } from 'react'
import { Lock, LogIn, Shield } from 'lucide-react'
import { api } from '../api/client'
import { Button, Card, Input, Label } from '../components/ui'

export function Login({ instanceName, onLoggedIn }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await api.login({ username, password })
      onLoggedIn(res)
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <Card className="w-full max-w-sm p-6">
        <div className="flex flex-col items-center text-center mb-6">
          <div
            style={{
              backgroundColor: 'var(--dv-accent-soft)',
              borderColor: 'var(--dv-accent-border)',
              color: 'var(--dv-accent-text)',
            }}
            className="flex h-12 w-12 items-center justify-center rounded-2xl border mb-3"
          >
            <Shield className="h-6 w-6" />
          </div>
          <h1 className="text-lg font-bold tracking-tight text-zinc-100">
            DockVault{instanceName ? ` · ${instanceName}` : ''}
          </h1>
          <p className="mt-1 text-xs text-zinc-400">
            Sign in to manage Docker backups and storage keys
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <Label htmlFor="login-user">Username</Label>
            <Input
              id="login-user"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              placeholder="admin"
              required
            />
          </div>
          <div>
            <Label htmlFor="login-pass">Password</Label>
            <Input
              id="login-pass"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              placeholder="••••••••"
              required
            />
          </div>

          {error && (
            <div className="flex items-center gap-2 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs text-rose-300">
              <Lock className="h-3.5 w-3.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          <Button type="submit" className="w-full" disabled={loading}>
            <LogIn className="h-4 w-4" />
            {loading ? 'Signing in...' : 'Sign In'}
          </Button>
        </form>
      </Card>
    </div>
  )
}
