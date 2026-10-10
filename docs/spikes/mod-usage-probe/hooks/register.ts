import type { Register } from 'claude-code'

// Rows are shape and counts only: key names, token counts, ids, never message text or any credential.
const lines: string[] = []

const keys = (v: unknown) => (v && typeof v === 'object' ? Object.keys(v as object).sort() : [])

async function record($: any, row: Record<string, unknown>) {
  const id = await $.session.id()
  lines.push(JSON.stringify({ at: await $.clock.now(), session_id: id, ...row }))
  const dir = (await $.env.get('USAGE_PROBE_OUT')) ?? '/tmp'
  await $.fs.write(`${dir}/usage-probe-${id}.jsonl`, lines.join('\n') + '\n')
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await record($, { ev: 'session.start', eventKeys: keys(e), surface: e.surface })
    return next(e)
  })

  on('turn.step', async function* ($, e, next) {
    const r = yield* next(e)
    await record($, {
      ev: 'turn.step', eventKeys: keys(e), resultKeys: keys(r), agentId: e.agentId ?? null,
      model: e.model, usage: r.usage, stopReason: r.stopReason,
    })
    return r
  })

  on('turn.complete', async ($, e, next) => {
    const u = await $.session.usage()
    await record($, {
      ev: 'turn.complete', eventKeys: keys(e), agentId: e.agentId ?? null, reason: e.reason,
      usage: e.usage ?? null, sessionUsageKeys: keys(u), sessionCost: u.cost ?? null,
    })
    return next(e)
  })

  on('session.append', { door: 'response' }, async ($, e, next) => {
    const r = await next(e)
    await record($, {
      ev: 'session.append', eventKeys: keys(e), messageKeys: keys(e.message), uuid: e.uuid,
      agentId: e.agentId ?? null, resultKeys: keys(r),
    })
    return r
  })

  on('session.end', async ($, e, next) => {
    let fetched: unknown = null
    try {
      const res = await $.http.fetch('https://example.com/', { method: 'HEAD' })
      fetched = { ok: res.ok, status: res.status }
    } catch (err) {
      fetched = { error: String((err as Error)?.message ?? err).slice(0, 200) }
    }
    // A header taken from the environment reaches the server verbatim; the echo reports only that it arrived.
    const key = await $.env.get('USAGE_PROBE_KEY')
    let echoed: unknown = null
    if (key) {
      try {
        const res = await $.http.fetch('https://httpbin.org/headers', { headers: { authorization: `Bearer ${key}` } })
        const seen = (JSON.parse(res.text).headers ?? {}).Authorization
        echoed = { status: res.status, arrived: seen === `Bearer ${key}` }
      } catch (err) {
        echoed = { error: String((err as Error)?.message ?? err).slice(0, 200) }
      }
    }
    await record($, { ev: 'session.end', reason: e.reason, httpFetch: fetched, envKeyHeader: echoed })
    return next(e)
  })
}
