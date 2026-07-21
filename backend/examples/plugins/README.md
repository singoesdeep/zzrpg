# Example plugins — extending *this* RPG's hooks/events

These are standalone, unregistered example plugins (not wired into `cmd/server`
or `cmd/gamedemo` — read their source, or `go test ./...` them) showing how a
**third-party plugin extends this repo's specific RPG** through the sdk plugin
mechanics, without touching engine or game code:

- **[`xpboost`](xpboost/plugin.go)** — a hook filter (double gold rewards), a
  hook action/veto (block attacks on a protected target), an event
  subscription (react to kills), and an HTTP route, all in one plugin.
- **[`achievements`](achievements/plugin.go)** — a purely event-driven,
  stateful plugin: no hooks, just subscriptions, its own in-memory state, and a
  service other plugins could query.

**This is different from gamekit's extensibility story.** These two extend
*this RPG's* hooks (`character.HookRewards`, `combat.HookPreAttack`, quest
events) — they're specific to `backend/game/*`. For extending a **gamekit**
toolkit instead (the pattern a new game built on gamekit would actually use),
see `backend/plugins/idlekit` + `backend/plugins/buildings` and
[`docs/GETTING_STARTED.md`](../../../docs/GETTING_STARTED.md) instead — that
pairing is the maintained, documented reference.

See [`docs/PLUGIN_GUIDE.md`](../../../docs/PLUGIN_GUIDE.md) for the sdk plugin
mechanics (hooks, events, routes, migrations) both of these examples exercise.
