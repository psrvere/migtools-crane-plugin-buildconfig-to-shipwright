# Decision kinds

Every decision question a skill puts to the human opens with its kind from this table.

| Kind | What it settles | Example from this repo |
|---|---|---|
| Scope | What this story does and what it leaves to another | BUILD-2315 kept one of its seven items |
| Architecture | Which component or repo owns the change: the plugin, the strategy, or upstream Shipwright | BUILD-1491 adds a `build-env` parameter to the buildah strategy (strategy-catalog PR #31), and the plugin only maps entries to it (BUILD-2499) |
| Mapping | How one BuildConfig field is written into the Shipwright Build | `binary: {}` becomes a Local source (BUILD-2475) |
| Policy | What the plugin does with something it can't convert: warn, fail, skip or pass through | Custom strategy BuildConfigs pass through with a reason |
| Compatibility | Which target clusters the output must work on, and whether shipped output may change | Keeping Shipwright v0.19 so Go 1.25 still builds |
| Interface | Anything a person or script reads: flags, annotations, warning text | `--imagestream-mapping` flag format |
| Security | Where credentials and secret values flow | Proxy URLs that embed passwords copied into `spec.env` |
| Code structure | How the change is laid out in code: a shared helper, a new `process*` step, or inline | Sharing the build-args loop with a new env loop |
| Testing | What proves the change works: which goldens, fixtures and cluster runs | Golden per fixture versus a unit test only |

Scope and architecture come first, because they decide which repo and which story. Mapping,
policy, compatibility and interface are what users notice. Code structure and testing rarely
need the human; decide them and show them in the spec or plan. A question may name a second
kind when it has one ("mapping, with a compatibility side"), and say in one clause why it is
not a nearby kind when that could confuse.
